package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	genType        string
	genScope       string
	genDesc        string
	genAITool      string
	genVersion     string
	genEnv         string
	genStrategy    string
	genOutput      string
	genPRNum       int
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate ODS-compliant templates",
	Long:  `Generate templates and scaffold files compliant with Open Delivery Spec schemas.`,
}

var generateBranchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Generate a branch name",
	RunE: func(cmd *cobra.Command, args []string) error {
		name := fmt.Sprintf("%s/%s", genType, genDesc)
		fmt.Println(name)
		return nil
	},
}

var generateCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Generate a commit message template",
	RunE: func(cmd *cobra.Command, args []string) error {
		scopePart := ""
		if genScope != "" {
			scopePart = fmt.Sprintf("(%s)", genScope)
		}
		msg := fmt.Sprintf("%s%s: %s\n\n", genType, scopePart, genDesc)
		if genAITool != "" {
			msg += fmt.Sprintf("AI-assisted: true\nAI-tool: %s\nAI-review: pending\nAI-confidence: medium\n", genAITool)
		} else {
			msg += "AI-assisted: false\n"
		}
		writeOutput(msg)
		return nil
	},
}

var generatePRCmd = &cobra.Command{
	Use:   "pr",
	Short: "Generate a PR description template",
	RunE: func(cmd *cobra.Command, args []string) error {
		tmpl := fmt.Sprintf(`## Summary
[Brief description of changes]

## Type
- [ ] Feature
- [ ] Bugfix
- [ ] Hotfix
- [ ] Refactor
- [ ] Documentation
- [ ] Chore

## AI Disclosure
- [ ] This PR contains AI-generated code
- **AI Tool:** %s
- **AI Scope:** [What the AI generated]
- **Human Review:** [What the human reviewed]

## Changes
- [List key changes]

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated
- [ ] Manual testing performed

## Risk Assessment
- **Deployment risk:** [Low / Medium / High]
- **Rollback plan:** [Link or brief description]

## Checklist
- [ ] Branch naming follows ODS
- [ ] Commits follow ODS
- [ ] AI-generated code has been reviewed by a human
- [ ] No secrets or credentials included
`, genAITool)
		writeOutput(tmpl)
		return nil
	},
}

var generateRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Generate a rollback plan template",
	RunE: func(cmd *cobra.Command, args []string) error {
		plan := map[string]interface{}{
			"release_id":                  genVersion,
			"rollback_strategy":           genStrategy,
			"estimated_rollback_time_minutes": 5,
			"tested":                      false,
			"steps": []map[string]interface{}{
				{"step": 1, "action": "rollback_step_1", "description": "Describe rollback action", "verification": "How to verify success"},
			},
			"rollback_indicators": map[string]interface{}{
				"error_rate_threshold": "> 1% for 5 minutes",
				"monitoring_dashboard": "[Grafana dashboard URL]",
				"alert_channel":        "#team-alerts",
			},
			"data_rollback": map[string]interface{}{
				"database_migrations":    false,
				"migration_reversible":   true,
				"data_loss_risk":         "none",
				"backup_taken":           true,
			},
			"communication_plan": map[string]interface{}{
				"notification_template": "Rolling back [RELEASE] due to [REASON]. ETA: [TIME] minutes.",
			},
		}
		data, _ := json.MarshalIndent(plan, "", "  ")
		writeOutput(string(data))
		return nil
	},
}

var generateReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Generate a release readiness report template",
	RunE: func(cmd *cobra.Command, args []string) error {
		report := map[string]interface{}{
			"release_id":         genVersion,
			"target_environment": genEnv,
			"timestamp":          time.Now().Format(time.RFC3339),
			"ready":              false,
			"overall_score":      0,
			"gates": map[string]interface{}{
				"ci":              map[string]interface{}{"passed": false, "required": true},
				"tests":           map[string]interface{}{"passed": false, "required": true},
				"security_scan":   map[string]interface{}{"passed": false, "required": true},
				"approvals":       map[string]interface{}{"passed": false, "required": true},
				"rollback_plan":   map[string]interface{}{"passed": false, "required": true},
				"breaking_changes": map[string]interface{}{"passed": false, "required": true},
			},
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		writeOutput(string(data))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.AddCommand(generateBranchCmd)
	generateCmd.AddCommand(generateCommitCmd)
	generateCmd.AddCommand(generatePRCmd)
	generateCmd.AddCommand(generateRollbackCmd)
	generateCmd.AddCommand(generateReleaseCmd)

	// branch
	generateBranchCmd.Flags().StringVarP(&genType, "type", "t", "feature", "branch type (feature|bugfix|hotfix|release|chore)")
	generateBranchCmd.Flags().StringVarP(&genDesc, "description", "d", "", "branch description in kebab-case")

	// commit
	generateCommitCmd.Flags().StringVarP(&genType, "type", "t", "feat", "commit type (feat|fix|chore|docs|refactor|test|perf|ci|build|revert)")
	generateCommitCmd.Flags().StringVarP(&genScope, "scope", "s", "", "commit scope")
	generateCommitCmd.Flags().StringVarP(&genDesc, "description", "d", "", "short description")
	generateCommitCmd.Flags().StringVar(&genAITool, "ai-tool", "", "AI tool name (e.g. 'GitHub Copilot')")

	// pr
	generatePRCmd.Flags().StringVar(&genAITool, "ai-tool", "", "AI tool name")

	// rollback
	generateRollbackCmd.Flags().StringVar(&genVersion, "version", "v0.1.0", "release version")
	generateRollbackCmd.Flags().StringVar(&genStrategy, "strategy", "feature_flag", "rollback strategy")

	// release
	generateReleaseCmd.Flags().StringVar(&genVersion, "version", "v0.1.0", "release version")
	generateReleaseCmd.Flags().StringVar(&genEnv, "env", "staging", "target environment")

	// shared output flag
	for _, c := range []*cobra.Command{generateCommitCmd, generatePRCmd, generateRollbackCmd, generateReleaseCmd} {
		c.Flags().StringVarP(&genOutput, "output", "o", "", "output file (default: stdout)")
	}
}

func writeOutput(content string) {
	if genOutput != "" {
		os.WriteFile(genOutput, []byte(content), 0644)
		fmt.Printf("Written to %s\n", genOutput)
		return
	}
	fmt.Print(content)
}
