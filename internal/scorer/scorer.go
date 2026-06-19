// Package scorer computes the technical debt impact of a code change.
// It combines AI detection, quality analysis, test coverage, and duplication
// signals into a single technical debt delta score.
package scorer

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/detector"
)

// ScoreResult holds the full technical debt scoring output.
type ScoreResult struct {
	PRNumber            int                `json:"pr_number,omitempty"`
	TechnicalDebtDelta  float64            `json:"technical_debt_delta"`
	Breakdown           ScoreBreakdown     `json:"breakdown"`
	Verdict             string             `json:"verdict"` // "decrease", "neutral", "increase"
	Recommendation      string             `json:"recommendation"`
	FilesAnalyzed       int                `json:"files_analyzed"`
}

// ScoreBreakdown provides the dimensional scores.
type ScoreBreakdown struct {
	AICodeRatio     float64 `json:"ai_code_ratio"`      // AI lines / total lines
	DefectDensity   float64 `json:"defect_density"`     // issues per KLOC
	CriticalIssues  int     `json:"critical_issues"`    // critical + high severity
	TestCoverage    float64 `json:"test_coverage"`      // test lines / total lines
	DuplicationRate float64 `json:"duplication_rate"`   // estimated duplication
}

// Options configures scoring behavior.
type Options struct {
	// DetectorResult from AI code detection.
	DetectorResult *detector.DetectionResult
	// AnalyzerResult from quality analysis.
	AnalyzerResult *analyzer.AnalysisResult
	// TestFiles is the number of lines in test files changed.
	TestLines int
	// TotalChangedLines is the total lines changed in the PR.
	TotalChangedLines int
}

// Score computes the technical debt score from available inputs.
func Score(opts Options) *ScoreResult {
	result := &ScoreResult{}

	br := ScoreBreakdown{}

	// Dimension 1: AI code ratio (AI lines / total)
	if opts.TotalChangedLines > 0 && opts.DetectorResult != nil {
		aiLines := 0
		for _, f := range opts.DetectorResult.Files {
			aiLines += f.AILines
		}
		if aiLines == 0 && opts.DetectorResult.AIGenerated {
			// No file-level data but AI detected: estimate from confidence
			aiLines = int(float64(opts.TotalChangedLines) * opts.DetectorResult.Confidence * 0.5)
		}
		br.AICodeRatio = float64(aiLines) / float64(opts.TotalChangedLines)
	}

	// Dimension 2: Defect density (issues per KLOC)
	if opts.AnalyzerResult != nil && opts.AnalyzerResult.TotalLines > 0 {
		br.DefectDensity = opts.AnalyzerResult.IssueDensity()
		br.CriticalIssues = opts.AnalyzerResult.CriticalCount()
	}

	// Dimension 3: Test coverage ratio
	if opts.TotalChangedLines > 0 {
		br.TestCoverage = float64(opts.TestLines) / float64(opts.TotalChangedLines)
	}

	// Dimension 4: Duplication rate (estimated via git)
	if opts.TotalChangedLines > 0 {
		br.DuplicationRate = estimateDuplication()
	}

	result.Breakdown = br

	// Compute weighted tech debt delta
	// Higher = worse (increasing debt)
	delta := 0.0
	delta += br.AICodeRatio * 3.0      // AI code weight: 3 (highest concern)
	delta += br.DefectDensity * 2.0    // Defects weight: 2
	delta += float64(br.CriticalIssues) * 1.5 // Critical issues: 1.5 each

	// Only apply test coverage and duplication penalties when AI code is
	// detected or quality issues exist. For human-written PRs with no issues,
	// these metrics are not meaningful and would produce misleading verdicts.
	hasAIOrIssues := br.AICodeRatio > 0 || br.DefectDensity > 0 || br.CriticalIssues > 0
	if hasAIOrIssues {
		delta += (1.0 - br.TestCoverage) * 1.0  // Low coverage: up to 1.0 penalty
		delta += br.DuplicationRate * 1.0  // Duplication: 1.0
	}
	result.TechnicalDebtDelta = delta

	// Verdict
	switch {
	case delta <= 1.0:
		result.Verdict = "decrease"
		result.Recommendation = "Low risk — acceptable for merge"
	case delta <= 3.0:
		result.Verdict = "neutral"
		result.Recommendation = "Moderate risk — review recommended, ensure adequate tests"
	case delta <= 5.0:
		result.Verdict = "increase"
		result.Recommendation = "High risk — add tests, reduce AI contribution ratio, fix critical issues"
	default:
		result.Verdict = "increase"
		result.Recommendation = "Block — critical technical debt increase. Address critical issues and add test coverage before merge."
	}

	result.FilesAnalyzed = 0
	if opts.DetectorResult != nil {
		result.FilesAnalyzed = len(opts.DetectorResult.Files)
	}

	if result.FilesAnalyzed == 0 && opts.AnalyzerResult != nil {
		result.FilesAnalyzed = 1 // at least something was analyzed
	}

	return result
}

// estimateDuplication looks at git diff to estimate code duplication rate.
// Simple heuristic: duplicate lines / total lines in the diff.
func estimateDuplication() float64 {
	out, err := exec.Command("git", "diff", "HEAD~1").Output()
	if err != nil {
		return 0
	}

	lines := strings.Split(string(out), "\n")
	added := make(map[string]int)
	totalAdded := 0

	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			content := line[1:]
			trimmed := strings.TrimSpace(content)
			if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
				continue
			}
			added[trimmed]++
			totalAdded++
		}
	}

	if totalAdded == 0 {
		return 0
	}

	duplicates := 0
	for _, count := range added {
		if count > 1 {
			duplicates += count - 1
		}
	}

	return float64(duplicates) / float64(totalAdded)
}

// TrendPoint is a single data point for trend analysis.
type TrendPoint struct {
	Date                string  `json:"date"`
	TechnicalDebtDelta  float64 `json:"technical_debt_delta"`
	IsAIPR              bool    `json:"is_ai_pr"`
}

// Trend computes a trend over multiple PRs.
func Trend(points []TrendPoint) string {
	if len(points) == 0 {
		return "No data available"
	}

	aiSum := 0.0
	aiCount := 0
	humanSum := 0.0
	humanCount := 0

	for _, p := range points {
		if p.IsAIPR {
			aiSum += p.TechnicalDebtDelta
			aiCount++
		} else {
			humanSum += p.TechnicalDebtDelta
			humanCount++
		}
	}

	aiAvg := 0.0
	if aiCount > 0 {
		aiAvg = aiSum / float64(aiCount)
	}
	humanAvg := 0.0
	if humanCount > 0 {
		humanAvg = humanSum / float64(humanCount)
	}

	return fmt.Sprintf("AI PRs avg: +%.1f tech debt | Human PRs avg: %.1f | Delta: %+.1f (over %d PRs)",
		aiAvg, humanAvg, aiAvg-humanAvg, len(points))
}

// FormatScore returns a human-readable score summary.
func (r *ScoreResult) FormatScore() string {
	b := r.Breakdown
	return fmt.Sprintf(
		"Tech Debt Delta: %+.1f | AI Ratio: %.0f%% | Defects: %.1f/KLOC | Critical: %d | Coverage: %.0f%% | Duplication: %.0f%%",
		r.TechnicalDebtDelta,
		b.AICodeRatio*100,
		b.DefectDensity,
		b.CriticalIssues,
		b.TestCoverage*100,
		b.DuplicationRate*100,
	)
}
