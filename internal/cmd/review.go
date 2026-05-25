package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
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
	Short: "AI change review workflow",
	Long:  `Generate, validate, and analyze AI change review records across L1/L2/L3 levels.`,
}

var reviewGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate an AI change review record (L1/L2/L3)",
	Long: `Generate an ODS-compliant AI change review record with the correct checklist for the specified level.

Review levels:
  L1 — Quick Scan: AI < 20% of diff, standard review checklist
  L2 — Enhanced Review: AI 20-80% of diff, expanded checklist with security + AI-specific checks
  L3 — Full Audit: AI > 80% of diff, full checklist + architecture, documentation, compliance, second reviewer

Examples:
  ods review generate --pr 42 --level L2 --ai-pct 45
  ods review generate --pr 99 --level L3 --ai-pct 92`,
	RunE: func(cmd *cobra.Command, args []string) error {
		level := reviewLevel
		if level == "" {
			// Auto-determine from ai-pct
			level = levelFromPct(reviewAIPct)
		}

		record := buildReviewRecord(reviewPR, level, reviewAIPct)
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling review record: %w", err)
		}
		fmt.Println(string(data))
		return nil
	},
}

func levelFromPct(pct float64) string {
	if pct < 20 {
		return "L1"
	} else if pct <= 80 {
		return "L2"
	}
	return "L3"
}

func buildReviewRecord(pr int, level string, aiPct float64) map[string]interface{} {
	record := map[string]interface{}{
		"pr_number":                   pr,
		"review_level":                level,
		"ai_contribution_percentage":  math.Round(aiPct*10) / 10,
		"reviewer":                    "<your-github-username>",
		"timestamp":                   time.Now().Format(time.RFC3339),
		"outcome":                     "pending",
		"checklist_results":           buildChecklistResults(level),
		"issues_found":                []interface{}{},
		"human_modifications":         []interface{}{},
	}

	if level == "L3" {
		record["second_reviewer"] = "<second-reviewer-username>"
	}

	return record
}

func buildChecklistResults(level string) map[string]interface{} {
	switch level {
	case "L1":
		return map[string]interface{}{
			"correctness": map[string]interface{}{"passed": false, "issues": 0},
			"quality":     map[string]interface{}{"passed": false, "issues": 0},
		}
	case "L2":
		return map[string]interface{}{
			"correctness": map[string]interface{}{"passed": false, "issues": 0},
			"security":    map[string]interface{}{"passed": false, "issues": 0},
			"ai_specific": map[string]interface{}{"passed": false, "issues": 0},
			"quality":     map[string]interface{}{"passed": false, "issues": 0},
		}
	case "L3":
		return map[string]interface{}{
			"correctness":   map[string]interface{}{"passed": false, "issues": 0},
			"security":      map[string]interface{}{"passed": false, "issues": 0},
			"ai_specific":   map[string]interface{}{"passed": false, "issues": 0},
			"quality":       map[string]interface{}{"passed": false, "issues": 0},
			"architecture":  map[string]interface{}{"passed": false, "issues": 0},
			"documentation": map[string]interface{}{"passed": false, "issues": 0},
			"compliance":    map[string]interface{}{"passed": false, "issues": 0},
		}
	default:
		return buildChecklistResults("L2")
	}
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
	Long: `Analyze a diff or commit log to estimate the AI contribution percentage for a PR.

In a real implementation, this analyzes:
  1. AI-assisted footers in commit messages
  2. AI Scope declarations in PR descriptions
  3. Diff analysis: lines attributed to AI vs human
  4. Git blame metadata (when available)

Currently estimates from commit message footers passed via stdin.

Examples:
  git log origin/main..HEAD --format=%B | ods review ai-percentage --pr 42`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInputIfAvailable()
		if err != nil {
			return err
		}

		pct, aiLines, totalLines := estimateAIPercentage(body)
		level := levelFromPct(pct)

		fmt.Printf("PR #%d AI Contribution Analysis\n", reviewPR)
		fmt.Printf("  AI-attributed lines: %d\n", aiLines)
		fmt.Printf("  Total changed lines: %d\n", totalLines)
		fmt.Printf("  AI percentage:        %.0f%%\n", pct)
		fmt.Printf("  Review level required: %s\n", level)
		fmt.Println()

		if level == "L3" {
			fmt.Println("⚠️  L3 Full Audit required — second reviewer mandatory")
		} else if level == "L2" {
			fmt.Println("ℹ️  L2 Enhanced Review recommended — AI-specific checklist required")
		} else {
			fmt.Println("✅ L1 Quick Scan — standard review sufficient")
		}

		return nil
	},
}

func estimateAIPercentage(commitLog string) (float64, int, int) {
	if strings.TrimSpace(commitLog) == "" {
		return 0, 0, 0
	}

	aiAssisted := 0
	totalCommits := 0

	// Parse commit log for AI-assisted markers and estimate
	lines := strings.Split(commitLog, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "AI-assisted:") {
			totalCommits++
			if strings.Contains(trimmed, "true") {
				aiAssisted++
			}
		}
	}

	if totalCommits == 0 {
		return 0, 0, 0
	}

	pct := float64(aiAssisted) / float64(totalCommits) * 100
	return pct, aiAssisted, totalCommits
}

func readInputIfAvailable() (string, error) {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return readInput()
	}
	return "", nil
}

func init() {
	rootCmd.AddCommand(reviewCmd)

	reviewCmd.AddCommand(reviewGenerateCmd)
	reviewCmd.AddCommand(reviewValidateCmd)
	reviewCmd.AddCommand(reviewAIPctCmd)

	reviewGenerateCmd.Flags().IntVarP(&reviewPR, "pr", "p", 0, "PR number")
	reviewGenerateCmd.Flags().StringVarP(&reviewLevel, "level", "l", "", "review level (L1|L2|L3) — auto-detected from percentage if not set")
	reviewGenerateCmd.Flags().Float64Var(&reviewAIPct, "ai-pct", 0, "AI contribution percentage (0-100)")

	reviewAIPctCmd.Flags().IntVarP(&reviewPR, "pr", "p", 0, "PR number")
	reviewAIPctCmd.Flags().Float64Var(&reviewAIPct, "ai-pct", 0, "AI contribution percentage (0-100) — for demo use without commit log input")

	reviewValidateCmd.Flags().StringVarP(&validateFile, "file", "f", "", "review record JSON file")
	reviewValidateCmd.Flags().BoolVar(&validateStdin, "stdin", false, "read from stdin")
}
