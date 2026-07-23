package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/report"
	"github.com/spf13/cobra"
)

var (
	reportSince   string
	reportJSON    bool
	reportHTML    string
	reportMaxCmts int
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
  ods report --json                   # machine-readable
  ods report --html ai-report.html    # shareable dashboard`,
	RunE: runReport,
}

func init() {
	rootCmd.AddCommand(reportCmd)
	reportCmd.Flags().StringVar(&reportSince, "since", "90 days ago",
		"git history window (any git --since expression)")
	reportCmd.Flags().IntVar(&reportMaxCmts, "max-commits", 0,
		"cap the number of commits scanned (0 = no cap)")
	reportCmd.Flags().BoolVar(&reportJSON, "json", false, "output as JSON")
	reportCmd.Flags().StringVar(&reportHTML, "html", "",
		"write a shareable HTML dashboard to this path (use - for stdout)")
}

func runReport(cmd *cobra.Command, args []string) error {
	logx.Debugf("report: collecting history since %q (max %d)", reportSince, reportMaxCmts)
	r, err := report.Collect(report.Options{Since: reportSince, MaxCommits: reportMaxCmts})
	if err != nil {
		return err
	}
	logx.Debugf("report: %d commits (%d AI, %d human)", r.TotalCommits, r.AICommits, r.HumanCommits)

	if reportJSON {
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling report: %w", err)
		}
		cmd.OutOrStdout().Write(data)
		cmd.OutOrStdout().Write([]byte("\n"))
		return nil
	}

	if reportHTML != "" {
		doc := report.RenderHTML(*r, time.Now())
		if reportHTML == "-" {
			cmd.OutOrStdout().Write([]byte(doc))
			return nil
		}
		if err := os.WriteFile(reportHTML, []byte(doc), 0o644); err != nil {
			return fmt.Errorf("writing HTML report to %s: %w", reportHTML, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Wrote AI attribution dashboard to %s\n", reportHTML)
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
	return nil
}
