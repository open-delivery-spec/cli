package cmd

import (
	"fmt"

	"github.com/open-delivery-spec/cli/internal/report"
	"github.com/spf13/cobra"
)

var badgeCmd = &cobra.Command{
	Use:   "badge",
	Short: "Generate a dynamic ODS compliance badge",
	Long: `Generate a compliance badge for your repository.

Outputs a JSON endpoint compatible with shields.io dynamic badges,
showing the current compliance status and score.

Examples:
  ods badge                  # Generate shields.io JSON to stdout
  ods badge --output badge.json  # Write to file`,
	RunE: func(cmd *cobra.Command, args []string) error {
		inputs := report.DiscoverInputs()
		r := report.Build(inputs, report.Options{})

		// Generate shields.io endpoint JSON
		statusColor := "brightgreen"
		statusLabel := "compliant"
		switch r.Status {
		case report.StatusCompliantWithWarnings:
			statusColor = "yellow"
			statusLabel = "warnings"
		case report.StatusNonCompliant:
			statusColor = "red"
			statusLabel = "non-compliant"
		}

		message := fmt.Sprintf("%s %d/100", statusLabel, r.Score)

		fmt.Printf(`{
  "schemaVersion": 1,
  "label": "ODS",
  "message": "%s",
  "color": "%s",
  "namedLogo": "check",
  "style": "flat"
}
`, message, statusColor)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(badgeCmd)
}
