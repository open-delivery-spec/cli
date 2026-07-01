package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/report"
	"github.com/spf13/cobra"
)

var (
	reportSince   string
	reportJSON    bool
	reportMaxCmts int
	reportLedger  string
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Summarize AI-assisted vs human work over recent history",
	Long: `Aggregate AI attribution across recent git history — a governance view of
how much delivered work is AI-assisted, and trending which way.

Attribution comes from the Co-Authored-By trailers AI tools emit automatically
(the same signal as ods detect), so this reflects what the tools disclose, not
forensic detection. It reports commit and changed-line shares plus a per-tool
breakdown over a window.

Examples:
  ods report                          # last 90 days
  ods report --since "30 days ago"    # custom window
  ods report --json                   # machine-readable`,
	RunE: runReport,
}

func init() {
	rootCmd.AddCommand(reportCmd)
	reportCmd.Flags().StringVar(&reportSince, "since", "90 days ago",
		"git history window (any git --since expression)")
	reportCmd.Flags().IntVar(&reportMaxCmts, "max-commits", 0,
		"cap the number of commits scanned (0 = no cap)")
	reportCmd.Flags().BoolVar(&reportJSON, "json", false, "output as JSON")
	reportCmd.Flags().StringVar(&reportLedger, "ledger", "",
		"append-only ledger file (.ods/ledger.jsonl) to add real quality/debt trends")
}

func runReport(cmd *cobra.Command, args []string) error {
	logx.Debugf("report: collecting history since %q (max %d)", reportSince, reportMaxCmts)
	r, err := report.Collect(report.Options{Since: reportSince, MaxCommits: reportMaxCmts})
	if err != nil {
		return err
	}
	logx.Debugf("report: %d commits (%d AI, %d human)", r.TotalCommits, r.AICommits, r.HumanCommits)

	// When a ledger is supplied, enrich the git-attribution report with real
	// quality/debt trends captured across past runs.
	if reportLedger != "" {
		trends, err := report.LoadTrends(reportLedger)
		if err != nil {
			return fmt.Errorf("reading ledger: %w", err)
		}
		r.Trends = &trends
		logx.Debugf("report: ledger trends over %d run(s)", trends.Runs)
	}

	if reportJSON {
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling report: %w", err)
		}
		cmd.OutOrStdout().Write(data)
		cmd.OutOrStdout().Write([]byte("\n"))
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "ODS AI Attribution Report — since %s\n\n", r.Since)
	if r.TotalCommits == 0 {
		fmt.Fprintln(out, "No commits in the selected window.")
		return nil
	}
	fmt.Fprintf(out, "  Commits:        %d total · %d AI-assisted (%.0f%%) · %d human\n",
		r.TotalCommits, r.AICommits, r.AICommitShare*100, r.HumanCommits)
	fmt.Fprintf(out, "  Changed lines:  %d total · %d AI-assisted (%.0f%%)\n",
		r.TotalChangedLines, r.AIChangedLines, r.AILineShare*100)
	if breakdown := r.ToolBreakdown(); len(breakdown) > 0 {
		fmt.Fprintf(out, "\n  By tool:\n")
		for _, tc := range breakdown {
			fmt.Fprintf(out, "    %-20s %d commit(s)\n", tc.Tool, tc.Commits)
		}
	}
	fmt.Fprintf(out, "\n%s\n", r.Summary)

	if r.Trends != nil {
		printTrends(out, r.Trends)
	}
	return nil
}

// printTrends renders the ledger-derived quality/debt history beneath the
// git-attribution summary.
func printTrends(out io.Writer, t *report.Trends) {
	fmt.Fprintf(out, "\nQuality trends (ledger) — %d run(s)\n", t.Runs)
	if t.Runs == 0 {
		fmt.Fprintln(out, "  No ledger history yet.")
		return
	}
	fmt.Fprintf(out, "  Tech debt:      net %+.1f · avg %+.1f per run\n", t.NetDebtDelta, t.AvgDebtDelta)
	fmt.Fprintf(out, "  High-risk runs: %d of %d (%.0f%%)\n", t.HighRiskRuns, t.Runs, t.HighRiskRate*100)
	if t.CoverageRuns > 0 {
		fmt.Fprintf(out, "  Coverage:       %.0f%% → %.0f%% (%+.0f pts over %d measured run(s))\n",
			t.FirstCoverage*100, t.LatestCoverage*100, t.CoverageDelta*100, t.CoverageRuns)
	}
	if t.AIRuns > 0 && t.HumanRuns > 0 {
		fmt.Fprintf(out, "  Defect density: AI %.1f vs human %.1f /KLOC\n",
			t.AIDefectDensity, t.HumanDefectDensity)
	}
	fmt.Fprintf(out, "\n%s\n", t.Summary)
}
