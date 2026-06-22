package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initPlatform string

const agentsMD = `# AGENTS.md — ODS Compliance for AI Coding Agents

This file tells AI coding assistants (Claude Code, Cursor, GitHub Copilot, etc.)
how to work with Open Delivery Spec (ODS) in this repository.

## AI Disclosure

Claude Code, GitHub Copilot, and Cursor automatically add ` + "`Co-Authored-By`" + ` trailers
to commits. ODS detects these as AI attribution signals — no additional configuration needed.

For tools that do not emit ` + "`Co-Authored-By`" + ` automatically, add it to the commit footer:

` + "`" + `
Co-Authored-By: <AI Tool Name> <email>
` + "`" + `

Or use the ODS supplemental trailer fields:

` + "`" + `
AI-assisted: true
AI-tool: <tool-name>
AI-review: pending
` + "`" + `

## Quick Reference

` + "`" + `bash
# Detect AI code in your changes
ods detect

# Analyze AI code quality
ods analyze

# Score technical debt impact
ods score

# Check against enterprise policy
ods check
` + "`" + `

## Installation

` + "`" + `bash
go install github.com/open-delivery-spec/cli/cmd/ods@latest
ods init
ods hook install
` + "`" + `
`

const cursorRules = `# Cursor Rules — ODS Compliance

You are working in a repository that follows the Open Delivery Spec (ODS).

## AI Disclosure
Cursor automatically adds Co-Authored-By trailers to commits, which ODS detects
as AI attribution. No additional configuration needed.

For extra attribution metadata, you may add to the commit footer:
AI-scope: <what you generated>
AI-review: pending

## Commands
- ods detect  — Detect AI-generated code
- ods analyze — Analyze AI code quality
- ods score   — Score technical debt impact
- ods check   — Evaluate enterprise policy
`

const odsWorkflow = `name: ODS AI Code Quality
on:
  pull_request:
    types: [opened, synchronize, reopened]

jobs:
  ods:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
      - uses: open-delivery-spec/validate-action@v1
        with:
          diff-base: ${{ github.event.pull_request.base.sha }}
          pr-body: ${{ github.event.pull_request.body }}
          branch: ${{ github.head_ref }}
          commits: ${{ github.event.pull_request.commits }}
`

const defaultPolicy = `package ods.policy

# ODS policy for this repository.
# Edit the rules below to enforce your team's AI code quality requirements.
# Documentation: https://github.com/open-delivery-spec/spec

default allow := true

# Block AI code with critical quality issues.
deny[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d", [issue.rule, issue.file, issue.line])
}

# Warn when high-confidence AI code has multiple issues.
warn[msg] {
    input.ai_generated == true
    input.ai_confidence > 0.8
    count(input.issues) > 2
    msg = "High-confidence AI code with multiple quality issues — enhanced review recommended"
}
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold ODS configuration for a project",
	Long: `Initialize ODS in your repository with a single command.

Scaffolds:
  • CI workflow for automated AI code quality checks
  • .ods/policy.rego  — OPA Rego policy (edit to add custom rules)
  • AGENTS.md for AI agent integration
  • .cursor/rules/ods-compliance.mdc for Cursor AI

Examples:
  ods init`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	workflowsDir := filepath.Join(".github", "workflows")
	cursorRulesDir := filepath.Join(".cursor", "rules")
	odsDir := ".ods"

	dirs := []string{".github", workflowsDir, odsDir, cursorRulesDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(workflowsDir, "ods-ai-quality.yml"):     odsWorkflow,
		filepath.Join(odsDir, "policy.rego"):                  defaultPolicy,
		"AGENTS.md":                                           agentsMD,
		filepath.Join(cursorRulesDir, "ods-compliance.mdc"):   cursorRules,
	}

	for path, content := range files {
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
	fmt.Println("── ODS initialized ──")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Install git hooks: ods hook install")
	fmt.Println("  2. Edit .ods/policy.rego to add custom enforcement rules")
	fmt.Println("  3. Commit and push — ODS will run on your next PR")
	fmt.Println("  4. AI agents will pick up AGENTS.md and .cursor/rules/ automatically")

	return nil
}
