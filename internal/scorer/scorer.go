// Package scorer computes the technical debt impact of a code change.
// It combines AI detection, quality analysis, test coverage, and duplication
// signals into a single technical debt delta score.
package scorer

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/detector"
)

// ScoreResult holds the full technical debt scoring output.
type ScoreResult struct {
	TechnicalDebtDelta float64        `json:"technical_debt_delta"`
	Breakdown          ScoreBreakdown `json:"breakdown"`
	Verdict            string         `json:"verdict"` // direction of the delta: "increase", "neutral", "decrease"
	Risk               string         `json:"risk"`    // band of the delta: "low", "moderate", "high", "critical"
	Recommendation     string         `json:"recommendation"`
	FilesAnalyzed      int            `json:"files_analyzed"`
}

// ScoreBreakdown provides the dimensional scores.
type ScoreBreakdown struct {
	AICodeRatio        float64 `json:"ai_code_ratio"`        // AI lines / total changed code lines
	AICodeRatioSource  string  `json:"ai_code_ratio_source"` // git-ai/commit-trailer/diff-heuristics/unknown
	DefectDensity      float64 `json:"defect_density"`       // high/critical issues per KLOC (informational — not part of the delta)
	CriticalIssues     int     `json:"critical_issues"`      // critical + high severity
	TestCoverage       float64 `json:"test_coverage"`        // fraction in [0,1] or -1 if not measured
	TestCoverageSource string  `json:"test_coverage_source"` // go/lcov/cobertura/nyc/unknown
	DuplicationRate    float64 `json:"duplication_rate"`     // estimated duplication among added code lines
}

// Options configures scoring behavior.
type Options struct {
	// DetectorResult from AI code detection.
	DetectorResult *detector.DetectionResult
	// AnalyzerResult from quality analysis.
	AnalyzerResult *analyzer.AnalysisResult
	// TotalChangedLines is the total lines changed in the PR.
	TotalChangedLines int
	// CoverageResult carries a coverage fraction parsed from a real coverage
	// report. When nil, coverage is not measured (-1); it is never estimated.
	// When set, its Coverage field may itself be -1 for "not measured".
	CoverageResult *CoverageInput
	// DiffBase is the git ref the change is diffed against, used by the
	// duplication estimate. Empty falls back to ODS_DIFF_BASE, then HEAD~1.
	DiffBase string
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

	// Dimension 1: AI code ratio — AI lines / total changed code lines. The
	// numerator is measured (git-ai notes), attested (the lines AI-attributed
	// commits added), or estimated by the diff heuristics; it is never
	// invented. No per-file data means no ratio — 0 with source "unknown" —
	// not a number derived from the detection confidence.
	br.AICodeRatioSource = "unknown"
	if opts.TotalChangedLines > 0 && opts.DetectorResult != nil && len(opts.DetectorResult.Files) > 0 {
		aiLines := 0
		for _, f := range opts.DetectorResult.Files {
			aiLines += f.AILines
		}
		br.AICodeRatio = math.Min(1, float64(aiLines)/float64(opts.TotalChangedLines))
		br.AICodeRatioSource = ratioSource(opts.DetectorResult.Sources)
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

	// Dimension 3: Test coverage, from a parsed coverage report only. The -1
	// sentinel means "not measured" and skips the coverage-gap term. Nothing
	// is estimated from test-file line counts: that number is not coverage,
	// and it fed policies as if it were.
	if opts.CoverageResult != nil {
		br.TestCoverage = opts.CoverageResult.Coverage
		br.TestCoverageSource = opts.CoverageResult.Source
		if br.TestCoverageSource == "" {
			br.TestCoverageSource = "unknown"
		}
	} else {
		br.TestCoverage = -1
		br.TestCoverageSource = "unknown"
	}

	// Dimension 4: Duplication rate (estimated via git)
	if opts.TotalChangedLines > 0 {
		br.DuplicationRate = estimateDuplication(opts.DiffBase)
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

	// Verdict is the direction of the delta, as the schema defines it: a
	// positive delta is an increase however small. Risk is the band the delta
	// falls in. The two used to be one field, which labelled +0.1 "decrease".
	// "neutral" is a delta that rounds to 0.0, so the label never contradicts
	// the one-decimal number printed next to it.
	switch tenths := math.Round(delta * 10); {
	case tenths > 0:
		result.Verdict = "increase"
	case tenths < 0:
		result.Verdict = "decrease"
	default:
		result.Verdict = "neutral"
	}
	switch {
	case delta <= 1.0:
		result.Risk = "low"
		result.Recommendation = "Acceptable for merge"
	case delta <= 3.0:
		result.Risk = "moderate"
		result.Recommendation = "Review recommended, ensure adequate tests"
	case delta <= 5.0:
		result.Risk = "high"
		result.Recommendation = "Add tests and fix high/critical issues"
	default:
		result.Risk = "critical"
		result.Recommendation = "Fix high/critical issues and add test coverage before merge"
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

// ratioSource names the provenance of the per-file AI line counts behind
// ai_code_ratio, strongest first — the order in which the detector picks them.
func ratioSource(sources []string) string {
	has := map[string]bool{}
	for _, s := range sources {
		has[s] = true
	}
	switch {
	case has["git-ai-notes"]:
		return "git-ai"
	case has["commit-trailer"]:
		return "commit-trailer"
	case has["diff-heuristics"]:
		return "diff-heuristics"
	}
	return "unknown"
}

// estimateDuplication looks at the git diff to estimate the duplication rate
// of the *code* the change adds. diffBase is the range the rest of the
// pipeline used; empty falls back to ODS_DIFF_BASE (set by validate-action),
// then HEAD~1. Non-code files are excluded: repeated table rows in a README
// are not copy-pasted code, and a docs-only change has nothing to estimate.
func estimateDuplication(diffBase string) float64 {
	if diffBase == "" {
		diffBase = os.Getenv("ODS_DIFF_BASE")
	}
	if diffBase == "" {
		diffBase = "HEAD~1"
	}
	names, err := exec.Command("git", "diff", "--name-only", diffBase).Output()
	if err != nil {
		return 0
	}
	var codeFiles []string
	for _, name := range strings.Split(strings.TrimSpace(string(names)), "\n") {
		name = strings.TrimSpace(name)
		if name != "" && detector.IsCodeFile(name) {
			codeFiles = append(codeFiles, name)
		}
	}
	if len(codeFiles) == 0 {
		return 0
	}
	args := append([]string{"diff", diffBase, "--"}, codeFiles...)
	out, err := exec.Command("git", args...).Output()
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
		"Tech Debt Delta: %+.1f | Risk: %s | AI Ratio: %.0f%% | Defects: %.1f/KLOC | Critical: %d | Coverage: %s | Duplication: %.0f%%",
		r.TechnicalDebtDelta,
		r.Risk,
		b.AICodeRatio*100,
		b.DefectDensity,
		b.CriticalIssues,
		coverageStr,
		b.DuplicationRate*100,
	)
}
