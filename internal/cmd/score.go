package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/open-delivery-spec/cli/internal/scorer"
	"github.com/spf13/cobra"
)

var (
	scoreJSON         bool
	scoreFormat       string
	scoreCoverageFile string
	scoreSARIF        string
	scoreDiffBase     string
)

var scoreCmd = &cobra.Command{
	Use:   "score",
	Short: "Score technical debt impact of a change",
	Long: `Compute the technical debt delta for a code change by combining AI detection,
quality analysis, test coverage, and duplication signals.

Coverage is auto-detected from common report files (coverage.out, lcov.info,
coverage.xml, coverage-summary.json); use --coverage to name the file. Without
a report, coverage is "not measured" (-1) and the coverage-gap term is skipped;
it is never estimated.

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
	scoreCmd.Flags().StringVar(&scoreCoverageFile, "coverage", "", "coverage report file (auto-detected if not set)")
	scoreCmd.Flags().StringVar(&scoreSARIF, "sarif", "", "SARIF v2.1.0 file whose findings are merged into the score")
	scoreCmd.Flags().StringVar(&scoreDiffBase, "diff-base", "", "git ref to diff against (default: $ODS_DIFF_BASE or HEAD~1)")
}

func runScore(cmd *cobra.Command, args []string) error {
	p := runPipeline(cmd, resolveDiffBase(scoreDiffBase), scoreSARIF, scoreCoverageFile)
	scoreResult := p.Score

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

// riskIcon maps the score's risk band to the status icon. The icon follows the
// band, not the verdict: a +0.1 delta is an "increase" and still low risk.
func riskIcon(risk string) string {
	switch risk {
	case "critical":
		return "❌"
	case "high", "moderate":
		return "⚠️"
	}
	return "✅"
}

func printScoreSummary(cmd *cobra.Command, r *scorer.ScoreResult) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s  Technical Debt Score\n", riskIcon(r.Risk))
	fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", r.FormatScore())
	fmt.Fprintf(cmd.OutOrStdout(), "   Verdict: %s, %s risk (%s)\n", r.Verdict, r.Risk, r.Recommendation)
}

func printScoreDetail(cmd *cobra.Command, r *scorer.ScoreResult) {
	b := r.Breakdown

	fmt.Fprintf(cmd.OutOrStdout(), "%s  Technical Debt Score Report\n", riskIcon(r.Risk))
	fmt.Fprintln(cmd.OutOrStdout(), "────────────────────────────────────────────────────────────")
	fmt.Fprintf(cmd.OutOrStdout(), "Delta:      %+.1f\n", r.TechnicalDebtDelta)
	fmt.Fprintf(cmd.OutOrStdout(), "Verdict:    %s\n", r.Verdict)
	fmt.Fprintf(cmd.OutOrStdout(), "Risk:       %s\n", r.Risk)
	fmt.Fprintf(cmd.OutOrStdout(), "Recommendation: %s\n", r.Recommendation)
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "Breakdown:")
	coverageStr := "N/A (not measured)"
	if b.TestCoverage >= 0 {
		coverageStr = fmt.Sprintf("%.0f%% (source: %s)", b.TestCoverage*100, b.TestCoverageSource)
	}
	ratioStr := "N/A (not measured)"
	if b.AICodeRatioSource != "unknown" {
		ratioStr = fmt.Sprintf("%.0f%% (source: %s)", b.AICodeRatio*100, b.AICodeRatioSource)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  AI Code Ratio:      %s\n", ratioStr)
	fmt.Fprintf(cmd.OutOrStdout(), "  Defect Density:     %.1f / KLOC\n", b.DefectDensity)
	fmt.Fprintf(cmd.OutOrStdout(), "  Critical Issues:    %d\n", b.CriticalIssues)
	fmt.Fprintf(cmd.OutOrStdout(), "  Test Coverage:      %s\n", coverageStr)
	fmt.Fprintf(cmd.OutOrStdout(), "  Duplication Rate:   %.0f%%\n", b.DuplicationRate*100)
}
