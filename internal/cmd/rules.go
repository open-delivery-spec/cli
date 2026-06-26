package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/open-delivery-spec/cli/internal/rules"
	"github.com/spf13/cobra"
)

var rulesJSON bool

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "List the built-in AI code quality rules",
	Long: `List the ODS analysis rule catalogue.

The catalogue is the single source of truth for every rule the analyzer can
emit — each entry has a stable ID, default severity, category, and remediation
suggestion. Use --json to consume it programmatically.

Examples:
  ods rules          # human-readable table
  ods rules --json   # machine-readable catalogue`,
	RunE: runRules,
}

func init() {
	rootCmd.AddCommand(rulesCmd)
	rulesCmd.Flags().BoolVar(&rulesJSON, "json", false, "output as JSON")
}

func runRules(cmd *cobra.Command, args []string) error {
	catalogue := rules.All()

	if rulesJSON {
		data, err := json.MarshalIndent(catalogue, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling rules: %w", err)
		}
		cmd.OutOrStdout().Write(data)
		cmd.OutOrStdout().Write([]byte("\n"))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "ODS Analysis Rules (%d)\n", len(catalogue))
	for _, r := range catalogue {
		fmt.Fprintf(cmd.OutOrStdout(), "\n%s [%s] %s\n", severityIcon(r.DefaultSeverity), r.DefaultSeverity, r.ID)
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", r.Description)
		fmt.Fprintf(cmd.OutOrStdout(), "  → %s\n", r.Suggestion)
	}
	return nil
}
