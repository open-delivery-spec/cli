package cmd

import (
	"fmt"

	"github.com/open-delivery-spec/cli/internal/report"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var reportOutput string

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate an ODS compliance report",
	Long: `Generate an ODS L1 compliance report using repository and CI context.

The command is convention-first: it reads GitHub Actions environment data when
available and falls back to local git metadata. Missing PR-only context is
reported as skipped instead of requiring extra flags.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		outputDir := reportOutput
		if outputDir == "" {
			outputDir = viper.GetString("report.output")
		}
		if outputDir == "" {
			outputDir = "ods-report"
		}

		inputs := report.DiscoverInputs()
		result := report.Build(inputs, report.Options{Strict: strict, Check: viper.GetString("report.check")})
		if err := report.WriteFiles(result, outputDir); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}

		fmt.Printf("ODS compliance report written to %s\n", outputDir)
		fmt.Printf("Status: %s\n", result.Status)
		fmt.Printf("Score: %d / 100\n", result.Score)

		if result.Status == report.StatusNonCompliant {
			return fmt.Errorf("ODS compliance report is non-compliant")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reportCmd)
	reportCmd.Flags().StringVarP(&reportOutput, "output", "o", "", "output directory (default: ods-report)")
}
