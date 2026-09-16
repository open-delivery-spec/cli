package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/spf13/cobra"
)

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

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold ODS configuration for a project",
	Long: `Initialize ODS in your repository with a single command.

Scaffolds:
  • .github/workflows/ods-ai-quality.yml  — CI workflow for AI code quality checks
  • .ods/policy.rego                       — the built-in default policy, to edit

Examples:
  ods init`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	workflowsDir := filepath.Join(".github", "workflows")
	odsDir := ".ods"

	for _, d := range []string{".github", workflowsDir, odsDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(workflowsDir, "ods-ai-quality.yml"): odsWorkflow,
		filepath.Join(odsDir, "policy.rego"):              policy.DefaultRegoPolicy(),
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
	fmt.Println("  1. Edit .ods/policy.rego — it starts as the built-in default policy")
	fmt.Println("  2. Optional: run the same check before you push with the pre-commit hook")
	fmt.Println("     (see .pre-commit-hooks.yaml in github.com/open-delivery-spec/cli)")
	fmt.Println("  3. Commit and push — ODS will run on your next PR")

	return nil
}
