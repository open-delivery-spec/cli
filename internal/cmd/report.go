package cmd

import (
	"fmt"
	"strings"

	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/open-delivery-spec/cli/internal/report"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	reportOutput    string
	reportFormat    string
	reportShowDetails bool
	reportThreshold int
	reportChecks    string
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate an ODS compliance report",
	Long: `Generate an ODS L1 compliance report using repository and CI context.

The command is convention-first: it reads GitHub Actions environment data when
available and falls back to local git metadata. Missing PR-only context is
reported as skipped instead of requiring extra flags.

Use --profile to select a compliance policy: oss, enterprise, or regulated.

Output formats:
  terminal  - Human-readable summary on stdout (default)
  json      - Full report as JSON on stdout
  sarif     - SARIF v2.1.0 for GitHub Code Scanning integration
  html      - Standalone HTML report page
  markdown  - Markdown summary suitable for PR comments
  files     - Write all formats to an output directory

Examples:
  ods report                                    # Full report, terminal output
  ods report --format json                      # JSON on stdout
  ods report --format sarif --output results/   # SARIF to file
  ods report --checks ai-disclosure,required-ci # Select specific checks
  ods report --show-details                     # Detailed output
  ods report --threshold 85                     # Non-zero exit if score < 85`,
	RunE: runReport,
}

func init() {
	rootCmd.AddCommand(reportCmd)
	reportCmd.Flags().StringVarP(&reportOutput, "output", "o", "", "output directory (default: ods-report)")
	reportCmd.Flags().StringVarP(&reportFormat, "format", "f", "terminal",
		"output format: terminal, json, sarif, html, markdown, files")
	reportCmd.Flags().BoolVar(&reportShowDetails, "show-details", false, "show detailed check results with fix suggestions")
	reportCmd.Flags().IntVar(&reportThreshold, "threshold", 0, "exit with non-zero if score is below threshold (0-100)")
	reportCmd.Flags().StringVar(&reportChecks, "checks", "", "comma-separated list of check IDs to run (default: all)")
}

func runReport(cmd *cobra.Command, args []string) error {
	outputDir := reportOutput
	if outputDir == "" {
		outputDir = viper.GetString("report.output")
	}

	// Load policy from profile flag or config file
	p, err := policy.LoadPolicy()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load policy: %v (using defaults)\n", err)
	}

	inputs := report.DiscoverInputs()
	opts := report.Options{
		Strict: strict,
	}

	// Determine which checks to run
	// When --checks is specified, run each selected check and merge results
	selectedChecks := parseCheckList(reportChecks)
	var result report.Report

	if len(selectedChecks) > 0 {
		// Run selected checks individually and merge into one report
		var allChecks []report.Check
		for _, checkID := range selectedChecks {
			optsCopy := opts
			optsCopy.Check = checkID
			r := report.Build(inputs, optsCopy)
			allChecks = append(allChecks, r.Checks...)
		}
		result = report.Build(inputs, opts)
		result.Checks = allChecks
		result.Score, result.Status = report.SummarizeChecks(allChecks)
	} else {
		result = report.Build(inputs, opts)
	}

	// Ensure policy info is attached
	if p != nil {
		result.Policy = p
		result.PolicyProfile = p.Profile
	}

	// Handle output format
	switch reportFormat {
	case "json":
		data, err := report.FormatJSON(result)
		if err != nil {
			return err
		}
		cmd.OutOrStdout().Write(data)
	case "sarif":
		data, err := report.FormatSARIF(result)
		if err != nil {
			return err
		}
		cmd.OutOrStdout().Write(data)
	case "html":
		page, err := report.HTML(result)
		if err != nil {
			return err
		}
		cmd.OutOrStdout().Write([]byte(page))
	case "markdown":
		md, err := report.Markdown(result)
		if err != nil {
			return err
		}
		cmd.OutOrStdout().Write([]byte(md))
	case "files":
		if outputDir == "" {
			outputDir = "ods-report"
		}
		if err := report.WriteFiles(result, outputDir); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
		fmt.Printf("ODS compliance report written to %s\n", outputDir)
		fmt.Printf("Status: %s\n", result.Status)
		fmt.Printf("Score: %d / 100\n", result.Score)
		if p != nil {
			fmt.Printf("Policy: %s\n", p.Profile)
		}
	default: // terminal
		printTerminalReport(result, p, reportShowDetails)
	}

	// Threshold check
	if reportThreshold > 0 && result.Score < reportThreshold {
		return fmt.Errorf("ODS compliance score %d is below threshold %d", result.Score, reportThreshold)
	}

	if result.Status == report.StatusNonCompliant && reportThreshold == 0 {
		return fmt.Errorf("ODS compliance report is non-compliant")
	}
	return nil
}

func parseCheckList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func printTerminalReport(r report.Report, p *policy.Policy, showDetails bool) {
	// Status header
	statusEmoji := "✅"
	if r.Status == report.StatusNonCompliant {
		statusEmoji = "❌"
	} else if r.Status == report.StatusCompliantWithWarnings {
		statusEmoji = "⚠️"
	}

	fmt.Printf("%s  ODS Compliance Report\n", statusEmoji)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("Status: %s\n", r.Status)
	fmt.Printf("Score:  %d / 100\n", r.Score)
	if p != nil {
		fmt.Printf("Policy: %s\n", p.Profile)
	}
	if r.Repository != "" {
		fmt.Printf("Repo:   %s\n", r.Repository)
	}
	if r.Ref != "" {
		fmt.Printf("Ref:    %s\n", r.Ref)
	}
	fmt.Println()

	// Check results table
	fmt.Printf("%-32s %-12s %s\n", "Check", "Result", "Details")
	fmt.Println(strings.Repeat("─", 60))
	for _, check := range r.Checks {
		resultIcon := "➖"
		switch check.Status {
		case report.CheckPass:
			resultIcon = "✅"
		case report.CheckWarning:
			resultIcon = "⚠️"
		case report.CheckFail:
			resultIcon = "❌"
		}

		detail := ""
		if len(check.Errors) > 0 {
			detail = check.Errors[0]
		} else if len(check.Warnings) > 0 {
			detail = check.Warnings[0]
		} else if len(check.Notes) > 0 {
			detail = check.Notes[0]
		} else {
			detail = check.Value
		}

		// Truncate detail for terminal display
		if len(detail) > 50 {
			detail = detail[:47] + "..."
		}

		fmt.Printf("%-32s %s %-8s   %s\n", check.Name, resultIcon, check.Status, detail)
	}

	// Detailed view
	if showDetails {
		fmt.Println()
		fmt.Println("── Detailed Results ──")
		for _, check := range r.Checks {
			if check.Status == report.CheckPass && check.Status != report.CheckSkipped {
				continue
			}
			fmt.Println()
			fmt.Printf("🔍 %s (%s)\n", check.Name, check.ID)
			if len(check.Errors) > 0 {
				fmt.Println("  Errors:")
				for _, e := range check.Errors {
					fmt.Printf("    ❌ %s\n", e)
				}
			}
			if len(check.Warnings) > 0 {
				fmt.Println("  Warnings:")
				for _, w := range check.Warnings {
					fmt.Printf("    ⚠️  %s\n", w)
				}
			}
			if len(check.FixSuggestions) > 0 {
				fmt.Println("  Fix suggestions:")
				for i, fs := range check.FixSuggestions {
					fmt.Printf("    %d. %s\n", i+1, fs.Title)
					if fs.Description != "" {
						fmt.Printf("       %s\n", fs.Description)
					}
				}
			}
		}
	}

	// Score gauge
	fmt.Println()
	fmt.Printf("Score: [")
	barLen := 30
	filled := r.Score * barLen / 100
	for i := 0; i < barLen; i++ {
		if i < filled {
			fmt.Print("█")
		} else {
			fmt.Print("░")
		}
	}
	fmt.Printf("] %d/100\n", r.Score)
}


