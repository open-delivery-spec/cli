package cmd

import (
	"fmt"
	"os"

	"github.com/open-delivery-spec/cli/internal/ciparser"
	"github.com/spf13/cobra"
)

var (
	ciLogFile  string
	ciPipeline string
	ciRepo     string
	ciBranch   string
	ciCommit   string
	ciTrigger  string
)

var ciCmd = &cobra.Command{
	Use:   "ci",
	Short: "CI failure analysis with hallucination detection",
	Long: `Parse CI failure logs and produce structured ODS CI failure reports with AI hallucination detection.

Detects common AI hallucinations:
  - Non-existent functions and symbols
  - Wrong import paths
  - Incorrect default values
  - Fake URLs
  - Deprecated API usage`,
}

var ciParseCmd = &cobra.Command{
	Use:   "parse",
	Short: "Parse a CI log into ODS failure report with hallucination detection",
	Long: `Parse a CI failure log and produce a structured ODS CI Failure Report.

Automatically detects:
  - Failed stages and test failures
  - AI hallucination patterns (non-existent symbols, wrong imports, fake URLs, etc.)
  - AI contribution to failures
  - Suggested fixes with confidence levels

Examples:
  ods ci parse --file ci-output.log --pipeline build-12345
  cat ci-output.log | ods ci parse --stdin --pipeline build-12345 --repo org/my-service`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading CI log: %w", err)
		}

		report, err := ciparser.ParseLog(body, ciPipeline, ciRepo, ciBranch, ciCommit, ciTrigger)
		if err != nil {
			return fmt.Errorf("parsing CI log: %w", err)
		}

		jsonOutput, err := report.ToJSON()
		if err != nil {
			return fmt.Errorf("marshaling report: %w", err)
		}
		fmt.Println(jsonOutput)

		// Print hallucination summary to stderr
		if len(report.Hallucinations) > 0 {
			fmt.Fprintf(os.Stderr, "\n⚠️  Detected %d potential AI hallucination(s):\n", len(report.Hallucinations))
			for i, h := range report.Hallucinations {
				fmt.Fprintf(os.Stderr, "  %d. [%s] %s (confidence: %.0f%%)\n", i+1, h.Category, h.Description, h.Confidence*100)
			}
		}

		return nil
	},
}

var ciExplainCmd = &cobra.Command{
	Use:   "explain",
	Short: "Explain a CI failure in human-readable terms",
	Long: `Parse a CI failure log and explain it in human-readable terms, highlighting AI-related issues.

Examples:
  ods ci explain --file ci-output.log --pipeline build-12345`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading CI log: %w", err)
		}

		report, err := ciparser.ParseLog(body, ciPipeline, ciRepo, ciBranch, ciCommit, ciTrigger)
		if err != nil {
			return fmt.Errorf("parsing CI log: %w", err)
		}

		fmt.Printf("Pipeline: %s\n", report.PipelineID)
		fmt.Printf("Status:   %s\n", report.Status)
		fmt.Println()

		for name, stage := range report.Stages {
			statusIcon := "✅"
			if stage.Status == "failed" {
				statusIcon = "❌"
			}
			fmt.Printf("  %s %s — %d failure(s)\n", statusIcon, name, len(stage.Failures))
			for _, f := range stage.Failures {
				aiTag := ""
				if f.AIRelated {
					aiTag = " 🤖 AI-related"
				}
				fmt.Printf("     • %s: %s%s\n", f.TestName, f.FailureType, aiTag)
			}
		}
		fmt.Println()

		if report.AISummary.LikelyAICaused {
			fmt.Printf("🤖 AI Summary (confidence: %s)\n", report.AISummary.Confidence)
			fmt.Printf("   %s\n", report.AISummary.Explanation)
		} else {
			fmt.Println("No AI-related issues detected.")
		}

		if len(report.Hallucinations) > 0 {
			fmt.Println()
			fmt.Printf("🔍 Hallucination Details (%d found):\n", len(report.Hallucinations))
			for _, h := range report.Hallucinations {
				fmt.Printf("   • %s: %s\n", h.Category, h.Description)
				if h.ClosestValidSymbol != "" {
					fmt.Printf("     Closest valid symbol: %s (distance: %d)\n", h.ClosestValidSymbol, h.LevenshteinDistance)
				}
			}
		}

		return nil
	},
}

var ciFixCmd = &cobra.Command{
	Use:   "fix-suggestions",
	Short: "Suggest fixes for CI failures",
	Long: `Analyze a CI failure log and suggest actionable fixes, prioritizing AI-caused issues.

Examples:
  ods ci fix-suggestions --file ci-output.log --pipeline build-12345`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading CI log: %w", err)
		}

		report, err := ciparser.ParseLog(body, ciPipeline, ciRepo, ciBranch, ciCommit, ciTrigger)
		if err != nil {
			return fmt.Errorf("parsing CI log: %w", err)
		}

		if len(report.FixSuggestions) == 0 {
			fmt.Println("No specific fix suggestions could be generated from this log.")
			return nil
		}

		fmt.Printf("Fix suggestions for pipeline %s:\n\n", report.PipelineID)
		for _, fix := range report.FixSuggestions {
			autoTag := ""
			if fix.AutoFixAvailable {
				autoTag = " [AUTO-FIX AVAILABLE]"
			}
			fileTag := ""
			if fix.File != "" {
				fileTag = fmt.Sprintf(" (%s)", fix.File)
			}
			fmt.Printf("  %d. [%s]%s %s%s\n", fix.Priority, priorityLabel(fix.Priority), autoTag, fix.Action, fileTag)
		}

		return nil
	},
}

func priorityLabel(priority int) string {
	switch {
	case priority <= 2:
		return "HIGH"
	case priority <= 4:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

func init() {
	rootCmd.AddCommand(ciCmd)

	ciCmd.AddCommand(ciParseCmd)
	ciCmd.AddCommand(ciExplainCmd)
	ciCmd.AddCommand(ciFixCmd)

	for _, c := range []*cobra.Command{ciParseCmd, ciExplainCmd, ciFixCmd} {
		c.Flags().StringVarP(&validateFile, "file", "f", "", "CI log file path")
		c.Flags().BoolVar(&validateStdin, "stdin", false, "read from stdin")
		c.Flags().StringVarP(&ciPipeline, "pipeline", "p", "", "pipeline ID (required)")
		c.Flags().StringVar(&ciRepo, "repo", "", "repository (org/name)")
		c.Flags().StringVar(&ciBranch, "branch", "", "branch name")
		c.Flags().StringVar(&ciCommit, "commit", "", "commit SHA")
		c.Flags().StringVar(&ciTrigger, "trigger", "push", "trigger type (push|pull_request|schedule|manual)")
	}
}
