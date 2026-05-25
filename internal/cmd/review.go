package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/open-delivery-spec/cli/internal/validator"
	"github.com/spf13/cobra"
)

var (
	reviewPR    int
	reviewLevel string
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "AI change review workflow",
	Long:  `Generate, validate, and analyze AI change review records.`,
}

var reviewGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate an AI change review record template",
	RunE: func(cmd *cobra.Command, args []string) error {
		record := map[string]interface{}{
			"pr_number":                   reviewPR,
			"review_level":                "L2",
			"ai_contribution_percentage":  50,
			"reviewer":                    "your-username",
			"timestamp":                   time.Now().Format(time.RFC3339),
			"outcome":                     "pending",
			"checklist_results": map[string]interface{}{
				"correctness": map[string]interface{}{"passed": false, "issues": 0},
				"security":    map[string]interface{}{"passed": false, "issues": 0},
				"ai_specific": map[string]interface{}{"passed": false, "issues": 0},
				"quality":     map[string]interface{}{"passed": false, "issues": 0},
			},
			"issues_found":        []interface{}{},
			"human_modifications": []interface{}{},
		}
		data, _ := json.MarshalIndent(record, "", "  ")
		fmt.Println(string(data))
		return nil
	},
}

var reviewValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate an AI change review record against ODS schema",
	Long: `Validate an AI change review JSON record against the ODS AI Change Review schema.

Examples:
  ods review validate --file review.json
  cat review.json | ods review validate --stdin`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading review record: %w", err)
		}
		result, err := validator.ValidateAIReview(body)
		if err != nil {
			return err
		}
		return printResult(result)
	},
}

var reviewAIPctCmd = &cobra.Command{
	Use:   "ai-percentage",
	Short: "Calculate AI contribution percentage for a PR",
	RunE: func(cmd *cobra.Command, args []string) error {
		// In a real implementation, this would analyze git diff
		percentage := 0
		fmt.Printf("AI contribution for PR #%d: %d%%\n", reviewPR, percentage)
		fmt.Println("Review level required: L1 (Quick Scan)")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reviewCmd)

	reviewCmd.AddCommand(reviewGenerateCmd)
	reviewCmd.AddCommand(reviewValidateCmd)
	reviewCmd.AddCommand(reviewAIPctCmd)

	for _, c := range []*cobra.Command{reviewGenerateCmd, reviewAIPctCmd} {
		c.Flags().IntVarP(&reviewPR, "pr", "p", 0, "PR number")
	}

	reviewValidateCmd.Flags().StringVarP(&validateFile, "file", "f", "", "review record JSON file")
	reviewValidateCmd.Flags().BoolVar(&validateStdin, "stdin", false, "read from stdin")
}
