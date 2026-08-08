package cmd

import (
	"os"

	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/version"
	"github.com/spf13/cobra"
)

var debugFlag bool

var rootCmd = &cobra.Command{
	Use:   "ods",
	Short: "Open Delivery Spec — AI code quality gate",
	Long: `ods — Detect AI-generated code, analyze its quality,
score technical debt impact, and enforce enterprise policy.

Commands:
  detect   Detect AI-generated code
  analyze  Analyze AI code quality
  score    Score technical debt impact
  check    Evaluate OPA Rego policy
  attest   Emit an AI-code evidence document (CycloneDX)
  report   Summarize AI-assisted vs human work over recent history
  rules    List the built-in AI code quality rules
  hook     Install git hooks
  init     Scaffold ODS configuration

Use --debug (or set ODS_DEBUG=1) to print decision diagnostics to stderr.`,
	Version: version.Value,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if debugFlag || os.Getenv("ODS_DEBUG") != "" {
			logx.SetEnabled(true)
		}
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false,
		"enable debug logging to stderr (also via ODS_DEBUG=1)")
	// Subcommands register themselves via their own init() functions
}
