package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/coverage"
	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/ledger"
	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/sarif"
	"github.com/open-delivery-spec/cli/internal/scorer"
	"github.com/spf13/cobra"
)

var (
	scoreJSON         bool
	scoreFormat       string
	scoreTestDir      string
	scoreCoverageFile string
	scoreSARIF        string
	scoreDiffBase     string
	scoreLedgerAppend string
	scorePRNumber     int
)

var scoreCmd = &cobra.Command{
	Use:   "score",
	Short: "Score technical debt impact of a change",
	Long: `Compute the technical debt delta for a code change by combining AI detection,
quality analysis, test coverage, and duplication signals.

Coverage is auto-detected from common report files (coverage.out, lcov.info,
coverage.xml, coverage-summary.json). Use --coverage to specify the file
explicitly, or --test-dir to estimate coverage from test file line counts.

Examples:
  ods score                             # score the current diff
  ods score --json                      # JSON output
  ods score --format detail             # detailed breakdown
  ods score --coverage coverage.out     # explicit Go coverage file`,
	RunE: runScore,
}

func init() {
	rootCmd.AddCommand(scoreCmd)
	scoreCmd.Flags().BoolVar(&scoreJSON, "json", false, "output as JSON")
	scoreCmd.Flags().StringVar(&scoreFormat, "format", "summary", "output format: summary, detail, json")
	scoreCmd.Flags().StringVar(&scoreTestDir, "test-dir", "", "test directory (fallback line-count estimation)")
	scoreCmd.Flags().StringVar(&scoreCoverageFile, "coverage", "", "coverage report file (auto-detected if not set)")
	scoreCmd.Flags().StringVar(&scoreSARIF, "sarif", "", "SARIF v2.1.0 file whose findings are merged into the score")
	scoreCmd.Flags().StringVar(&scoreDiffBase, "diff-base", "", "git ref to diff against (default: $ODS_DIFF_BASE or HEAD~1)")
	scoreCmd.Flags().StringVar(&scoreLedgerAppend, "ledger-append", "", "append this run's result as one JSON line to the given append-only ledger file")
	scoreCmd.Flags().IntVar(&scorePRNumber, "pr", 0, "pull request number recorded in the ledger (0 = omit)")
}

func runScore(cmd *cobra.Command, args []string) error {
	base := resolveDiffBase(scoreDiffBase)
	// Keep the duplication estimator (which reads ODS_DIFF_BASE) on the same range.
	os.Setenv("ODS_DIFF_BASE", base)

	// Run detector
	detectOpts := detector.Options{DiffBase: base, MaxCommits: 10}
	detectResult, err := detector.Detect(detectOpts)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: detection failed: %v\n", err)
		detectResult = &detector.DetectionResult{}
	}

	// Run analyzer on changed files
	diffFiles, err := getGitDiffFiles(base)
	var analyzeResult *analyzer.AnalysisResult
	if err == nil && len(diffFiles) > 0 {
		analyzeResult = analyzer.Analyze(analyzer.Options{Files: diffFiles})
	} else {
		analyzeResult = &analyzer.AnalysisResult{}
	}

	// Merge external SARIF findings so the debt score reflects authoritative
	// analyzer results, not just the built-in heuristics.
	if scoreSARIF != "" {
		if iss, err := sarif.Load(scoreSARIF); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: loading SARIF file: %v\n", err)
		} else {
			analyzeResult.Issues = append(analyzeResult.Issues, iss...)
			analyzeResult.Summary = analyzer.ResummarizeSARIF(analyzeResult.Issues)
			logx.Debugf("score: merged %d SARIF finding(s) from %s", len(iss), scoreSARIF)
		}
	}

	// Count changed lines
	totalLines := 0
	for _, f := range detectResult.Files {
		totalLines += f.TotalLines
	}
	if totalLines == 0 && analyzeResult.TotalLines > 0 {
		totalLines = analyzeResult.TotalLines
	}
	if totalLines == 0 {
		totalLines = 1
	}

	// Detect test coverage from a coverage report file.
	var covResult coverage.Result
	if scoreCoverageFile != "" {
		covResult = coverage.Parse(scoreCoverageFile)
	} else {
		covResult = coverage.Detect(".")
	}

	// Fall back to test-dir line counting when no coverage file is available.
	testLines := 0
	if covResult.Coverage < 0 && scoreTestDir != "" {
		testLines = countTestDirLines(scoreTestDir)
	}

	var covInput *scorer.CoverageInput
	if covResult.Coverage >= 0 {
		covInput = &scorer.CoverageInput{
			Coverage: covResult.Coverage,
			Source:   string(covResult.Source),
		}
	}

	scoreResult := scorer.Score(scorer.Options{
		DetectorResult:    detectResult,
		AnalyzerResult:    analyzeResult,
		TestLines:         testLines,
		TotalChangedLines: totalLines,
		CoverageResult:    covInput,
	})

	logx.Debugf("score: delta=%.2f verdict=%s (ai_ratio=%.2f defect_density=%.2f critical=%d coverage=%.2f dup=%.2f)",
		scoreResult.TechnicalDebtDelta, scoreResult.Verdict,
		scoreResult.Breakdown.AICodeRatio, scoreResult.Breakdown.DefectDensity,
		scoreResult.Breakdown.CriticalIssues, scoreResult.Breakdown.TestCoverage,
		scoreResult.Breakdown.DuplicationRate)

	// Persist this run to the append-only ledger so `ods report` can trend the
	// quality/debt signals git history alone cannot reconstruct. A ledger write
	// failure is non-fatal: it must never fail the score itself.
	if scoreLedgerAppend != "" {
		if err := appendLedger(scoreLedgerAppend, base, detectResult, scoreResult); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: appending to ledger: %v\n", err)
		} else {
			logx.Debugf("score: appended run to ledger %s", scoreLedgerAppend)
		}
	}

	switch {
	case scoreJSON || scoreFormat == "json":
		data, err := json.MarshalIndent(scoreResult, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling result: %w", err)
		}
		cmd.OutOrStdout().Write(data)
		cmd.OutOrStdout().Write([]byte("\n"))
	case scoreFormat == "detail":
		printScoreDetail(cmd, scoreResult)
	default:
		printScoreSummary(cmd, scoreResult)
	}

	return nil
}

func printScoreSummary(cmd *cobra.Command, r *scorer.ScoreResult) {
	icon := "✅"
	if r.Verdict == "increase" {
		if r.TechnicalDebtDelta > 5.0 {
			icon = "❌"
		} else {
			icon = "⚠️"
		}
	} else if r.Verdict == "neutral" {
		icon = "⚠️"
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s  Technical Debt Score\n", icon)
	fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", r.FormatScore())
	fmt.Fprintf(cmd.OutOrStdout(), "   Verdict: %s (%s)\n", r.Verdict, r.Recommendation)
}

func printScoreDetail(cmd *cobra.Command, r *scorer.ScoreResult) {
	b := r.Breakdown
	icon := "✅"
	if r.Verdict == "increase" {
		icon = "❌"
	} else if r.Verdict == "neutral" {
		icon = "⚠️"
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s  Technical Debt Score Report\n", icon)
	fmt.Fprintln(cmd.OutOrStdout(), "────────────────────────────────────────────────────────────")
	fmt.Fprintf(cmd.OutOrStdout(), "Delta:      %+.1f\n", r.TechnicalDebtDelta)
	fmt.Fprintf(cmd.OutOrStdout(), "Verdict:    %s\n", r.Verdict)
	fmt.Fprintf(cmd.OutOrStdout(), "Recommendation: %s\n", r.Recommendation)
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "Breakdown:")
	coverageStr := "N/A (not measured)"
	if b.TestCoverage >= 0 {
		coverageStr = fmt.Sprintf("%.0f%% (source: %s)", b.TestCoverage*100, b.TestCoverageSource)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  AI Code Ratio:      %.0f%%\n", b.AICodeRatio*100)
	fmt.Fprintf(cmd.OutOrStdout(), "  Defect Density:     %.1f / KLOC\n", b.DefectDensity)
	fmt.Fprintf(cmd.OutOrStdout(), "  Critical Issues:    %d\n", b.CriticalIssues)
	fmt.Fprintf(cmd.OutOrStdout(), "  Test Coverage:      %s\n", coverageStr)
	fmt.Fprintf(cmd.OutOrStdout(), "  Duplication Rate:   %.0f%%\n", b.DuplicationRate*100)
}

// appendLedger builds a ledger.Record from a scored run and appends it. The
// base/head SHAs and branch are read from git so the record is self-describing;
// missing git context (e.g. shallow or detached state) simply leaves those
// fields empty rather than failing the append.
func appendLedger(path, base string, det *detector.DetectionResult, sc *scorer.ScoreResult) error {
	rec := ledger.Record{
		PR:      scorePRNumber,
		BaseSHA: gitRevParse(base),
		HeadSHA: gitRevParse("HEAD"),
		Branch:  gitRevParse("--abbrev-ref", "HEAD"),

		Verdict:            sc.Verdict,
		TechnicalDebtDelta: sc.TechnicalDebtDelta,
		DefectDensity:      sc.Breakdown.DefectDensity,
		CriticalIssues:     sc.Breakdown.CriticalIssues,
		TestCoverage:       sc.Breakdown.TestCoverage,
		TestCoverageSource: sc.Breakdown.TestCoverageSource,
		DuplicationRate:    sc.Breakdown.DuplicationRate,
		AICodeRatio:        sc.Breakdown.AICodeRatio,
	}
	if det != nil {
		rec.AIGenerated = det.AIGenerated
		rec.AIConfidence = det.Confidence
	}
	return ledger.Append(path, rec)
}

// gitRevParse resolves a git ref to a short SHA (or returns the branch name for
// --abbrev-ref). Any error yields an empty string so callers can record "no git
// context" without special-casing.
func gitRevParse(args ...string) string {
	out, err := exec.Command("git", append([]string{"rev-parse", "--short"}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func countTestDirLines(dir string) int {
	out, err := exec.Command("find", dir, "-name", "*_test.go", "-o", "-name", "test_*.py").Output()
	if err != nil {
		return 0
	}
	count := 0
	for _, f := range strings.Split(string(out), "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		data, err := os.ReadFile(f)
		if err == nil {
			count += len(strings.Split(string(data), "\n"))
		}
	}
	return count
}
