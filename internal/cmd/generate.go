package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	genType   string
	genScope  string
	genDesc   string
	genAITool string
	genOutput string
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate ODS-compliant templates",
	Long:  `Generate templates and scaffold files compliant with Open Delivery Spec schemas.`,
}

var generateBranchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Generate a branch name",
	Long: `Generate a Conventional Branch compliant branch name.

Examples:
  ods generate branch --type feature --description "add-oauth-login"
  ods generate branch --type bugfix --description "fix-null-pointer"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := fmt.Sprintf("%s/%s", genType, genDesc)
		fmt.Println(name)
		return nil
	},
}

var generateCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Generate a commit message template",
	Long: `Generate a Conventional Commits compliant commit message with optional AI disclosure.

Examples:
  ods generate commit --type feat --scope auth --description "add OAuth login"
  ods generate commit --type feat --description "add OAuth login" --ai-tool "GitHub Copilot"`,
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
	Long: `Generate an ODS-compliant PR description template with AI Disclosure section.

Examples:
  ods generate pr
  ods generate pr --ai-tool "Claude" --output .github/PULL_REQUEST_TEMPLATE.md`,
	RunE: func(cmd *cobra.Command, args []string) error {
		aiToolSection := genAITool
		tmpl := fmt.Sprintf(`## Summary
<!-- Brief description of what this PR does and why. 1-3 sentences. -->

## Type
- [ ] Feature
- [ ] Bugfix
- [ ] Hotfix
- [ ] Refactor
- [ ] Documentation
- [ ] Chore

## AI Disclosure
<!-- Required. Remove the checkbox line if no AI was used. -->
- [ ] This PR contains AI-generated code
- **AI Tool:** %s
- **AI Scope:** <!-- What did AI generate? e.g. "auth module, token exchange logic, tests" -->
- **Human Review:** <!-- What did the human verify? e.g. "Verified OAuth spec compliance, PKCE handling" -->

## Changes
- 

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated
- [ ] Manual testing performed

## Risk Assessment
- **Deployment risk:** Low / Medium / High
- **Rollback plan:** <!-- e.g. "Feature flag: oauth-v2" or "Revert commit" -->
- **Breaking change:** Yes / No

## Checklist
- [ ] Branch naming follows ODS (<type>/<description>)
- [ ] Commit messages follow ODS (Conventional Commits + AI attribution)
- [ ] AI-generated code has been reviewed by a human
- [ ] No secrets or credentials included
`, aiToolSection)
		writeOutput(tmpl)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.AddCommand(generateBranchCmd)
	generateCmd.AddCommand(generateCommitCmd)
	generateCmd.AddCommand(generatePRCmd)

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

	// shared output flag
	for _, c := range []*cobra.Command{generateCommitCmd, generatePRCmd} {
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
