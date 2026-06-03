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
	reviewAIPct float64
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "AI change review record management",
	Long: `Generate, validate, and manage AI change review records.

ODS uses qualitative AI disclosure — reviewers describe the scope of AI
contribution (e.g., "OAuth token refresh logic") rather than computing
brittle percentage estimates. The review level (L1/L2/L3) is determined
by the reviewer's judgment, not an automated percentage.`,
}

var reviewGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate an AI change review record template (L1/L2/L3)",
	Long: `Generate an ODS-compliant AI change review record with the correct checklist for the specified level.

Review levels guide the depth of review based on how much AI contributed:
  L1 — Quick Scan: Minor AI assistance, standard review checklist
  L2 — Enhanced Review: Significant AI contribution, expanded checklist with security + AI checks
  L3 — Full Audit: AI generated most of the change, full checklist + architecture, second reviewer

Examples:
  ods review generate --pr 42 --level L2
  ods review generate --pr 99 --level L3`,
	RunE: func(cmd *cobra.Command, args []string) error {
		level := reviewLevel
		if level == "" {
			level = "L2"
		}

		record := buildReviewRecord(reviewPR, level)
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling review record: %w", err)
		}
		fmt.Println(string(data))
		return nil
	},
}

func buildReviewRecord(pr int, level string) map[string]interface{} {
	record := map[string]interface{}{
		"pr_number":         pr,
		"review_level":      level,
		"reviewer":          "<your-github-username>",
		"timestamp":         time.Now().Format(time.RFC3339),
		"outcome":           "pending",
		"checklist_results": buildChecklistResults(level),
		"issues_found":      []interface{}{},
		"human_modifications": []interface{}{},
		"ai_disclosure": map[string]interface{}{
			"scope":   "<Describe the scope of AI-generated code, e.g. 'auth module, token exchange'>",
			"tool":    "<AI tool used, e.g. 'GitHub Copilot, Claude'>",
			"review_notes": "<What the human reviewed and verified>",
		},
	}

	if level == "L3" {
		record["second_reviewer"] = "<second-reviewer-username>"
	}

	return record
}

func buildChecklistResults(level string) map[string]interface{} {
	base := map[string]interface{}{
		"correctness": map[string]interface{}{"passed": false, "issues": 0},
		"quality":     map[string]interface{}{"passed": false, "issues": 0},
	}

	if level == "L2" || level == "L3" {
		base["security"] = map[string]interface{}{"passed": false, "issues": 0}
		base["ai_specific"] = map[string]interface{}{"passed": false, "issues": 0}
	}

	if level == "L3" {
		base["architecture"] = map[string]interface{}{"passed": false, "issues": 0}
		base["documentation"] = map[string]interface{}{"passed": false, "issues": 0}
		base["compliance"] = map[string]interface{}{"passed": false, "issues": 0}
	}

	return base
}

var reviewValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate an AI change review record against ODS schema",
	Long: `Validate an AI change review JSON record against the ODS AI Change Review schema.

The validator checks:
  - All required fields (pr_number, review_level, reviewer, timestamp, outcome, checklist_results)
  - Valid review_level values (L1, L2, L3)
  - Valid outcome values (approved, approved_with_changes, changes_requested, blocked)
  - Checklist completeness per level
  - L3 requires second_reviewer
  - approved_with_changes / changes_requested require human_modifications

Examples:
  ods review validate --file review.json
  cat review.json | ods review validate --stdin`,
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

func init() {
	rootCmd.AddCommand(reviewCmd)

	reviewCmd.AddCommand(reviewGenerateCmd)
	reviewCmd.AddCommand(reviewValidateCmd)

	reviewGenerateCmd.Flags().IntVarP(&reviewPR, "pr", "p", 0, "PR number")
	reviewGenerateCmd.Flags().StringVarP(&reviewLevel, "level", "l", "L2", "review level (L1|L2|L3)")

	reviewValidateCmd.Flags().StringVarP(&validateFile, "file", "f", "", "review record JSON file")
	reviewValidateCmd.Flags().BoolVar(&validateStdin, "stdin", false, "read from stdin")
}
