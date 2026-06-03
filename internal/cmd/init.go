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
<!-- Required. Remove the checkbox line if no AI was used. -->
- [ ] This PR contains AI-generated code
- **AI Tool:** <!-- e.g. GitHub Copilot, Claude, Cursor -->
- **AI Scope:** <!-- What did AI generate? e.g. "auth module, token exchange, tests" -->
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
# See https://open-delivery-spec.github.io/spec for full documentation.

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

const agentsMD = `# AGENTS.md — ODS Compliance Instructions for AI Coding Agents

This file tells AI coding assistants (Claude Code, Cursor, GitHub Copilot, etc.)
how to comply with Open Delivery Spec (ODS) when contributing to this repository.

## Branch Naming

Create branches using the Conventional Branch format:

` + "`" + `<type>/<description>` + "`" + `

Valid types: feature, bugfix, hotfix, release, chore
Description must be lowercase, kebab-case (hyphens, no underscores, no spaces).

Examples:
- ` + "`" + `feature/add-oauth-login` + "`" + `
- ` + "`" + `bugfix/fix-null-pointer` + "`" + `
- ` + "`" + `chore/update-dependencies` + "`" + `

You can generate a valid branch name with:
` + "`" + `ods generate branch --type feature --description "add-oauth-login"` + "`" + `

## Commit Messages

Use Conventional Commits format:

` + "`" + `<type>[scope]: <description>` + "`" + `

Valid types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert

### AI Disclosure in Commits

When AI assists in generating code, add these trailers to the commit footer:

` + "`" + `
AI-assisted: true
AI-tool: <tool-name>
AI-review: pending
AI-confidence: medium
` + "`" + `

Valid AI-tool values: GitHub Copilot, Claude, Cursor, etc.
Valid AI-review values: pending, passed, failed
Valid AI-confidence values: low, medium, high

## PR Description

Every PR must include these sections:

1. **## Summary** — 1-3 sentences about what and why
2. **## Type** — Feature, Bugfix, Hotfix, Refactor, Documentation, or Chore
3. **## AI Disclosure** — REQUIRED. Always include this section.
   - If AI was used: check the box and fill in AI Tool, AI Scope, Human Review
   - If no AI was used: include the section with the checkbox unchecked
4. **## Changes** — Bullet list of key changes
5. **## Testing** — What was tested and how
6. **## Checklist** — Standard compliance checklist

The AI Disclosure section must use qualitative descriptions:
- **AI Scope:** Describe what the AI generated (e.g., "auth module, token exchange logic, unit tests")
- **Human Review:** Describe what the human verified (e.g., "Verified against OAuth 2.0 spec, checked PKCE flow, reviewed error handling")
- Do NOT use percentage estimates — they are unreliable and misleading.

## Quick Reference

` + "`" + `bash
# Validate before committing
ods validate branch $(git branch --show-current)
ods validate commit --file <(git log -1 --format=%B)

# Generate compliant names and messages
ods generate branch --type feature --description "my-feature"
ods generate commit --type feat --scope auth --description "add login" --ai-tool "Claude"
ods generate pr

# Full compliance report
ods report
` + "`" + `

## Installation

` + "`" + `bash
# Install ODS CLI
go install github.com/open-delivery-spec/cli/cmd/ods@latest

# Initialize ODS in your repo
ods init

# Install git hooks for local validation
ods hook install
` + "`" + `
`

const cursorRules = `# Cursor Rules — ODS Compliance

You are working in a repository that follows the Open Delivery Spec (ODS).

## Branch Naming
Always use Conventional Branch format: <type>/<description>
- Type: feature, bugfix, hotfix, release, chore
- Description: lowercase, kebab-case
- Example: feature/add-oauth-login

## Commit Messages
Use Conventional Commits: <type>[scope]: <description>

When AI assists in generating code, add these trailers:
AI-assisted: true
AI-tool: Cursor
AI-review: pending
AI-confidence: medium

## PR Description
Always include these sections in PR descriptions:
1. ## Summary
2. ## Type
3. ## AI Disclosure (REQUIRED — always include)
4. ## Changes
5. ## Testing
6. ## Checklist

For AI Disclosure, use qualitative descriptions (not percentages):
- AI Scope: Describe what the AI generated
- Human Review: Describe what the human verified

## Commands
- ods validate branch <name> — Check branch name
- ods validate commit --file <file> — Check commit message
- ods report — Full compliance report
- ods fix — Get fix suggestions
- ods generate pr — Generate PR template
`

var initCmd = &cobra.Command{
	Use:   "init [platform]",
	Short: "Scaffold ODS configuration for a project",
	Long: `Initialize ODS in your repository with a single command.

Scaffolds:
  • PR template with AI Disclosure section
  • CI workflows for automated validation
  • Policy configuration file (.ods.yaml)
  • AGENTS.md for AI agent integration (Claude Code, Copilot, etc.)
  • .cursor/rules/ods-compliance.mdc for Cursor AI

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
	cursorRulesDir := filepath.Join(".cursor", "rules")

	dirs := []string{githubDir, workflowsDir, cursorRulesDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(githubDir, "PULL_REQUEST_TEMPLATE.md"):  prTemplate,
		filepath.Join(workflowsDir, "ods-l1.yml"):             odsWorkflowPR,
		filepath.Join(workflowsDir, "ods-commit-message.yml"): odsWorkflowCommit,
		".ods.yaml": odsConfig,
		"AGENTS.md": agentsMD,
		filepath.Join(cursorRulesDir, "ods-compliance.mdc"): cursorRules,
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
	fmt.Println("  3. Install git hooks: ods hook install")
	fmt.Println("  4. Commit and push — the ODS workflows will run on your next PR")
	fmt.Println("  5. AI agents will pick up AGENTS.md and .cursor/rules/ automatically")
	fmt.Println("  6. Optional: add an ODS badge to your README:")
	fmt.Println("     [![ODS](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/.../ods-report/ods-badge.json)](...)")
	fmt.Println()
	fmt.Println("  Docs: https://open-delivery-spec.github.io/spec")
	fmt.Println("  CLI:  https://github.com/open-delivery-spec/cli")

	return nil
}
