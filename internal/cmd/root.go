package cmd

import (
	"github.com/open-delivery-spec/cli/internal/version"
	"github.com/spf13/cobra"
)

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
  hook     Install git hooks
  init     Scaffold ODS configuration`,
	Version: version.Value,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Subcommands register themselves via their own init() functions
}
