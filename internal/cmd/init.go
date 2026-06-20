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
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version: "1.25"
      - name: Install ODS
        run: go install github.com/open-delivery-spec/cli/cmd/ods@latest
      - name: Detect AI code
        run: ods detect --json
      - name: Analyze AI code quality
        run: ods analyze --json
      - name: Score technical debt
        run: ods score --json
      - name: Evaluate policy
        run: ods check --json
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold ODS configuration for a project",
	Long: `Initialize ODS in your repository with a single command.

Scaffolds:
  • CI workflow for automated AI code quality checks
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

	dirs := []string{".github", workflowsDir, cursorRulesDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(workflowsDir, "ods-ai-quality.yml"): odsWorkflow,
		"AGENTS.md": agentsMD,
		filepath.Join(cursorRulesDir, "ods-compliance.mdc"): cursorRules,
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
	fmt.Println("  2. Create .ods/policy.rego for custom enterprise rules")
	fmt.Println("  3. Commit and push — ODS will run on your next PR")
	fmt.Println("  4. AI agents will pick up AGENTS.md and .cursor/rules/ automatically")

	return nil
}
