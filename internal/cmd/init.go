package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initPlatform string

const prTemplate = `## Summary
<!-- Brief description of what this PR does and why. 1-3 sentences. -->

## Type
<!-- Check one -->
- [ ] Feature
- [ ] Bugfix
- [ ] Hotfix
- [ ] Refactor
- [ ] Documentation
- [ ] Chore

## AI Disclosure
<!-- Required if AI assisted in generating any part of this PR. Remove if not applicable. -->
- [ ] This PR contains AI-generated code
- **AI Tool:** <!-- e.g. GitHub Copilot, Cursor, Claude -->
- **AI Scope:** <!-- What part did AI generate? e.g. "auth module, token exchange, tests" -->
- **Human Review:** <!-- What did the human verify? e.g. "Verified OAuth spec compliance, PKCE handling, redirect URI validation" -->

## Changes
<!-- Bullet list of key changes. Each line should describe one coherent change. -->
-
-

## Testing
<!-- Check all that apply. Add details. -->
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated
- [ ] Manual testing performed

## Risk Assessment
<!-- Required for hotfixes and high-risk changes. Optional otherwise. -->
- **Deployment risk:** <!-- Low / Medium / High -->
- **Rollback plan:** <!-- e.g. "Feature flag: ods-oauth-v2" or "Revert commit" -->
- **Breaking change:** <!-- Yes / No -->

## Checklist
- [ ] Branch naming follows ODS (<type>/<description>)
- [ ] Commit messages follow ODS (Conventional Commits + AI attribution if applicable)
- [ ] AI-generated code has been reviewed by a human
- [ ] No secrets or credentials included
- [ ] Documentation updated (if applicable)
`

const odsWorkflowPR = `name: ODS L1
on:
  pull_request:
    types: [opened, edited, synchronize, reopened]

permissions:
  contents: read
  pull-requests: write
  issues: write

jobs:
  ods:
    runs-on: ubuntu-latest
    steps:
      - uses: open-delivery-spec/validate-action@v1
        with:
          check: all
          branch_name: ${{ github.head_ref }}
          pr_body: ${{ github.event.pull_request.body }}
          strict: "true"
`

const odsWorkflowCommit = `name: ODS Commit Message
on: [push]

jobs:
  ods-commit:
    runs-on: ubuntu-latest
    steps:
      - uses: open-delivery-spec/validate-action@v1
        with:
          check: commit-message
          commit_message: ${{ github.event.head_commit.message }}
          strict: "true"
`

const odsConfig = `# ODS Compliance Policy Configuration
# This is the recommended starting configuration.
# See https://open-delivery-spec.dev for full documentation.

# Profile: oss, enterprise, or regulated
# - oss: Open-source friendly; AI disclosure optional, relaxed rules
# - enterprise: Full ODS L1 enforcement; AI disclosure required, commit scope required
# - regulated: Maximum compliance; tickets required, all AI rules strictly enforced
profile: enterprise

policy:
  branch:
    allowed_types:
      - feature
      - bugfix
      - hotfix
      - release
      - chore
    require_ticket: false
    max_description_length: 100

  pr:
    required_sections:
      - "## Summary"
      - "## Type"
      - "## AI Disclosure"
      - "## Changes"
      - "## Testing"
      - "## Checklist"
    min_changes: 1

  commit:
    allowed_types:
      - feat
      - fix
      - docs
      - style
      - refactor
      - perf
      - test
      - build
      - ci
      - chore
      - revert
    require_scope: true
    max_subject_length: 72

  ai_disclosure:
    required: true
    strict_tool_name: true
    require_human_review: true
    ai_branch_naming: warning

  severity:
    branch_type: error
    branch_format: error
    pr_sections: error
    pr_ai_disclosure: error
    pr_ai_tool: error
    commit_type: error
    commit_scope: warning
    commit_ai: error
`

var initCmd = &cobra.Command{
	Use:   "init [platform]",
	Short: "Scaffold ODS configuration for a project",
	Long: `Initialize ODS in your repository with a single command.

Scaffolds PR templates, CI workflows, and a policy configuration file
so you can adopt ODS L1 without reading the spec first.

Supported platforms:
  github    - GitHub Actions workflows + PR template + .ods.yaml

Examples:
  ods init github
  ods init github --output-dir .`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVarP(&initPlatform, "platform", "p", "github", "target CI platform (github)")
}

func runInit(cmd *cobra.Command, args []string) error {
	platform := initPlatform
	if len(args) > 0 {
		platform = args[0]
	}

	switch platform {
	case "github":
		return initGitHub()
	default:
		return fmt.Errorf("unsupported platform: %q (supported: github)", platform)
	}
}

func initGitHub() error {
	// .github/ directory
	githubDir := ".github"
	workflowsDir := filepath.Join(githubDir, "workflows")

	dirs := []string{githubDir, workflowsDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(githubDir, "PULL_REQUEST_TEMPLATE.md"): prTemplate,
		filepath.Join(workflowsDir, "ods-l1.yml"):            odsWorkflowPR,
		filepath.Join(workflowsDir, "ods-commit-message.yml"): odsWorkflowCommit,
		".ods.yaml": odsConfig,
	}

	for path, content := range files {
		// Check if file already exists
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("  ⏭️  Skipped (already exists): %s\n", path)
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		fmt.Printf("  ✅ Created: %s\n", path)
	}

	fmt.Println()
	fmt.Println("── ODS L1 scaffolding complete ──")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review .ods.yaml — adjust the profile and rules for your team")
	fmt.Println("  2. Review .github/PULL_REQUEST_TEMPLATE.md — customize sections if needed")
	fmt.Println("  3. Commit and push — the ODS workflows will run on your next PR")
	fmt.Println("  4. Optional: add an ODS badge to your README:")
	fmt.Println("     [![ODS L1](https://img.shields.io/badge/ODS-L1%20Structured%20Delivery-blue)](https://open-delivery-spec.dev)")
	fmt.Println()
	fmt.Println("  Docs: https://open-delivery-spec.dev")
	fmt.Println("  Adoption guide: https://open-delivery-spec.dev/adoption-guide")

	return nil
}
