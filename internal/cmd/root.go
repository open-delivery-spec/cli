package cmd

import (
	"fmt"
	"os"

	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/version"
	"github.com/spf13/cobra"
)

var debugFlag bool

var rootCmd = &cobra.Command{
	Use:   "ods",
	Short: "Open Delivery Spec — governance and visibility for AI-assisted code",
	Long: `ods — Attribute AI-assisted code from the signals tools volunteer, surface
quality findings, score technical debt impact, and enforce policy as code.

Commands:
  detect   Attribute AI-assisted code in a change
  analyze  Analyze AI code quality
  score    Score technical debt impact
  check    Evaluate OPA Rego policy
  attest   Emit an AI-code evidence document (CycloneDX)
  report   Summarize AI-assisted vs human work over recent history
  rules    List the built-in AI code quality rules
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
	rootCmd.SetVersionTemplate(fmt.Sprintf("ods {{.Version}} (commit %s, built %s)\n", version.Commit, version.Date))
	// Subcommands register themselves via their own init() functions
}
