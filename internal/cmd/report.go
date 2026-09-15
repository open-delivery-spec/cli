package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	reportRepo    string

	mergeJSON     bool
	mergeHTML     string
	mergeMarkdown string
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
  ods report --html ai-report.html    # shareable dashboard

For every repository of an organization, run this in each one with --json and
merge the results with "ods report merge".`,
	RunE: runReport,
}

var reportMergeCmd = &cobra.Command{
	Use:   "merge <report.json>...",
	Short: "Merge per-repository reports into one organization-wide report",
	Long: `Merge the JSON of several "ods report --json" runs — one per repository —
into a single organization-wide view: the same commit and changed-line shares
summed across repositories, the per-tool breakdown, the merged trend, and a
per-repository table.

The merge reads only the report files, never git, so the repositories can be
scanned wherever they live (a scheduled workflow, a laptop, another CI) and
merged anywhere. A report without a "repo" field is named after its file.

Examples:
  ods report merge reports/*.json                       # text summary
  ods report merge reports/*.json --json                # machine-readable
  ods report merge reports/*.json --html index.html     # shareable dashboard
  ods report merge reports/*.json --markdown summary.md # job summary / README`,
	Args: cobra.MinimumNArgs(1),
	RunE: runReportMerge,
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
	reportCmd.Flags().StringVar(&reportRepo, "repo", "",
		"repository name recorded in the report, e.g. owner/name (default: from the origin remote)")

	reportCmd.AddCommand(reportMergeCmd)
	reportMergeCmd.Flags().BoolVar(&mergeJSON, "json", false, "print the merged report as JSON")
	reportMergeCmd.Flags().StringVar(&mergeHTML, "html", "",
		"write a shareable HTML dashboard to this path (use - for stdout)")
	reportMergeCmd.Flags().StringVar(&mergeMarkdown, "markdown", "",
		"write a Markdown summary to this path (use - for stdout)")
}

func runReport(cmd *cobra.Command, args []string) error {
	logx.Debugf("report: collecting history since %q (max %d)", reportSince, reportMaxCmts)
	r, err := report.Collect(report.Options{Since: reportSince, MaxCommits: reportMaxCmts, Repo: reportRepo})
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

func runReportMerge(cmd *cobra.Command, args []string) error {
	var reports []report.Report
	for _, path := range args {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		var r report.Report
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		if r.Repo == "" {
			r.Repo = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		reports = append(reports, r)
	}
	logx.Debugf("report merge: %d report(s)", len(reports))
	o := report.Merge(reports)

	out := cmd.OutOrStdout()
	wrote := false
	if mergeHTML != "" {
		doc := report.RenderOrgHTML(o, time.Now())
		if mergeHTML == "-" {
			out.Write([]byte(doc))
		} else {
			if err := os.WriteFile(mergeHTML, []byte(doc), 0o644); err != nil {
				return fmt.Errorf("writing HTML report to %s: %w", mergeHTML, err)
			}
			fmt.Fprintf(out, "Wrote organization dashboard to %s\n", mergeHTML)
		}
		wrote = true
	}
	if mergeMarkdown != "" {
		md := report.RenderOrgMarkdown(o)
		if mergeMarkdown == "-" {
			out.Write([]byte(md))
		} else {
			if err := os.WriteFile(mergeMarkdown, []byte(md), 0o644); err != nil {
				return fmt.Errorf("writing Markdown summary to %s: %w", mergeMarkdown, err)
			}
			fmt.Fprintf(out, "Wrote organization summary to %s\n", mergeMarkdown)
		}
		wrote = true
	}
	if mergeJSON {
		data, err := json.MarshalIndent(o, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling report: %w", err)
		}
		out.Write(data)
		out.Write([]byte("\n"))
		return nil
	}
	if wrote {
		return nil
	}

	fmt.Fprintf(out, "ODS AI Attribution Report — organization — since %s\n\n", o.Since)
	if o.TotalCommits == 0 {
		fmt.Fprintf(out, "%s.\n", o.Summary)
		return nil
	}
	fmt.Fprintf(out, "  Repositories:   %d scanned · %d with AI-assisted commits\n", o.ReposCovered, o.ReposWithAI)
	fmt.Fprintf(out, "  Commits:        %d total · %d AI-assisted (%.0f%%) · %d human\n",
		o.TotalCommits, o.AICommits, o.AICommitShare*100, o.HumanCommits)
	fmt.Fprintf(out, "  Changed lines:  %d total · %d AI-assisted (%.0f%%)\n",
		o.TotalChangedLines, o.AIChangedLines, o.AILineShare*100)
	if breakdown := o.ToolBreakdown(); len(breakdown) > 0 {
		fmt.Fprintf(out, "\n  By tool:\n")
		for _, tc := range breakdown {
			fmt.Fprintf(out, "    %-20s %d commit(s)\n", tc.Tool, tc.Commits)
		}
	}
	fmt.Fprintf(out, "\n  By repository:\n")
	for _, r := range o.Repos {
		fmt.Fprintf(out, "    %-40s %5d commit(s) · %3.0f%% AI · %3.0f%% of lines\n",
			r.Repo, r.TotalCommits, r.AICommitShare*100, r.AILineShare*100)
	}
	fmt.Fprintf(out, "\n%s\n", o.Summary)
	return nil
}
