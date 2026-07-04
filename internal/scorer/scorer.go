// Package scorer computes the technical debt impact of a code change.
// It combines AI detection, quality analysis, test coverage, and duplication
// signals into a single technical debt delta score.
package scorer

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/detector"
)

// ScoreResult holds the full technical debt scoring output.
type ScoreResult struct {
	PRNumber           int            `json:"pr_number,omitempty"`
	TechnicalDebtDelta float64        `json:"technical_debt_delta"`
	Breakdown          ScoreBreakdown `json:"breakdown"`
	Verdict            string         `json:"verdict"` // "decrease", "neutral", "increase"
	Recommendation     string         `json:"recommendation"`
	FilesAnalyzed      int            `json:"files_analyzed"`
}

// ScoreBreakdown provides the dimensional scores.
type ScoreBreakdown struct {
	AICodeRatio        float64 `json:"ai_code_ratio"`        // AI lines / total lines
	DefectDensity      float64 `json:"defect_density"`       // high/critical issues per KLOC (informational — not part of the delta)
	CriticalIssues     int     `json:"critical_issues"`      // critical + high severity
	TestCoverage       float64 `json:"test_coverage"`        // fraction in [0,1] or -1 if not measured
	TestCoverageSource string  `json:"test_coverage_source"` // go/lcov/cobertura/nyc/estimated/unknown
	DuplicationRate    float64 `json:"duplication_rate"`     // estimated duplication
}

// Options configures scoring behavior.
type Options struct {
	// DetectorResult from AI code detection.
	DetectorResult *detector.DetectionResult
	// AnalyzerResult from quality analysis.
	AnalyzerResult *analyzer.AnalysisResult
	// TestLines is the number of lines in test files changed (used when CoverageResult is nil).
	TestLines int
	// TotalChangedLines is the total lines changed in the PR.
	TotalChangedLines int
	// CoverageResult carries a parsed coverage fraction from a real coverage file.
	// When nil, coverage is estimated from TestLines/TotalChangedLines (or marked -1 if
	// TestLines is also 0). When set, its Coverage field may be -1 for "not measured".
	CoverageResult *CoverageInput
}

// CoverageInput carries the parsed coverage fraction and its source.
type CoverageInput struct {
	// Coverage is in [0,1], or -1 when not measured.
	Coverage float64
	// Source identifies the parser that produced the value.
	Source string
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

	// Dimension 2: Defect density using only high/critical issues per KLOC.
	// Low/medium issues are surfaced as warnings but excluded from the blocking
	// delta metric because they produce explosive density numbers on small diffs.
	if opts.AnalyzerResult != nil {
		// Critical/high issues always count toward the gate and score. Defect
		// density is per-KLOC, so it needs a line count; an ingested SARIF
		// finding may carry no local lines yet must still register.
		br.CriticalIssues = opts.AnalyzerResult.CriticalCount()
		if opts.AnalyzerResult.TotalLines > 0 {
			br.DefectDensity = opts.AnalyzerResult.SeverityWeightedDensity()
		}
	}

	// Dimension 3: Test coverage.
	// Prefer an explicitly provided coverage result from a parsed file.
	// Fall back to estimating from test-file line counts in the diff.
	// -1 sentinel means "not measured" — the coverage penalty is skipped.
	switch {
	case opts.CoverageResult != nil:
		br.TestCoverage = opts.CoverageResult.Coverage
		br.TestCoverageSource = opts.CoverageResult.Source
		if br.TestCoverageSource == "" {
			br.TestCoverageSource = "unknown"
		}
	case opts.TotalChangedLines > 0 && opts.TestLines > 0:
		br.TestCoverage = float64(opts.TestLines) / float64(opts.TotalChangedLines)
		br.TestCoverageSource = "estimated"
	default:
		br.TestCoverage = -1
		br.TestCoverageSource = "unknown"
	}

	// Dimension 4: Duplication rate (estimated via git)
	if opts.TotalChangedLines > 0 {
		br.DuplicationRate = estimateDuplication()
	}

	result.Breakdown = br

	// Technical debt is driven by code *quality*, not by how much of the change
	// is AI-written. Quality signals (critical/high findings, coverage gap,
	// duplication) form the base debt. The AI ratio then acts as a bounded risk
	// multiplier — AI-authored defects and untested AI code carry more risk
	// because no human reasoned through them — but AI quantity alone never
	// creates debt. A clean, fully-AI change scores ~0.
	//
	// Defect density is deliberately NOT part of the delta. It is a per-KLOC
	// ratio whose denominator is the diff size, so one high finding in a
	// 20-line change would cost 100x the same finding in a 2000-line change —
	// punishing small PRs hardest, the exact opposite of the real risk. The
	// absolute critical/high count below already charges for every finding at
	// any diff size; density stays in the breakdown as an informational rate.
	//
	// The coverage-gap term is skipped when coverage was not measured (-1
	// sentinel) to avoid false-positive blocks where no coverage tool is set up.
	qualityDebt := 0.0
	qualityDebt += float64(br.CriticalIssues) * 1.5 // critical + high issues
	if br.TestCoverage >= 0 {
		qualityDebt += (1.0 - br.TestCoverage) * 1.0 // coverage gap, up to 1.0
	}
	qualityDebt += br.DuplicationRate * 1.0 // duplication

	// AI risk multiplier: 1.0 (no AI) … 1.5 (fully AI). Amplifies real quality
	// problems for AI-heavy changes without penalizing clean AI code.
	aiRiskMultiplier := 1.0 + 0.5*br.AICodeRatio
	delta := qualityDebt * aiRiskMultiplier
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
		result.Recommendation = "High risk — add tests and fix high/critical issues"
	default:
		result.Verdict = "increase"
		result.Recommendation = "Block — critical technical debt increase. Fix high/critical issues and add test coverage before merge."
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
// Reads ODS_DIFF_BASE from the environment (set by validate-action) so the
// same diff range is used here as in the rest of the pipeline.
func estimateDuplication() float64 {
	diffBase := os.Getenv("ODS_DIFF_BASE")
	if diffBase == "" {
		diffBase = "HEAD~1"
	}
	out, err := exec.Command("git", "diff", diffBase).Output()
	if err != nil {
		return 0
	}

	var added []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "++") {
			added = append(added, line[1:])
		}
	}
	return duplicationRate(added)
}

// minDuplicationLineLen is the minimum trimmed length for a line to count toward
// duplication. Below this, lines are short boilerplate (return nil, break,
// continue, case x:) whose repetition says nothing about real copy-paste.
const minDuplicationLineLen = 12

// isStructuralLine reports whether a line carries no meaningful logic for
// duplication purposes: blank lines, comments, lines made up solely of
// structural punctuation (braces/brackets/parens/commas), and very short
// boilerplate. Counting these inflates the duplication rate on any real diff,
// where closing braces and trivial returns repeat constantly.
func isStructuralLine(trimmed string) bool {
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
		return true
	}
	// Lines that are only structural punctuation: } ) ] }) }, etc.
	if strings.Trim(trimmed, "{}()[],; \t") == "" {
		return true
	}
	if len(trimmed) < minDuplicationLineLen {
		return true
	}
	return false
}

// duplicationRate estimates the fraction of duplicated *meaningful* lines among
// the added lines of a diff. Structural and trivial lines are skipped so the
// metric reflects real copy-paste rather than syntactic repetition.
//
// This is a deliberately conservative single-line heuristic; token-window clone
// detection (or delegating to a dedicated tool) is the planned proper fix.
func duplicationRate(addedLines []string) float64 {
	counts := make(map[string]int)
	total := 0
	for _, line := range addedLines {
		trimmed := strings.TrimSpace(line)
		if isStructuralLine(trimmed) {
			continue
		}
		counts[trimmed]++
		total++
	}
	if total == 0 {
		return 0
	}
	duplicates := 0
	for _, count := range counts {
		if count > 1 {
			duplicates += count - 1
		}
	}
	return float64(duplicates) / float64(total)
}

// FormatScore returns a human-readable score summary.
func (r *ScoreResult) FormatScore() string {
	b := r.Breakdown
	coverageStr := "N/A"
	if b.TestCoverage >= 0 {
		coverageStr = fmt.Sprintf("%.0f%%", b.TestCoverage*100)
	}
	return fmt.Sprintf(
		"Tech Debt Delta: %+.1f | AI Ratio: %.0f%% | Defects: %.1f/KLOC | Critical: %d | Coverage: %s | Duplication: %.0f%%",
		r.TechnicalDebtDelta,
		b.AICodeRatio*100,
		b.DefectDensity,
		b.CriticalIssues,
		coverageStr,
		b.DuplicationRate*100,
	)
}
