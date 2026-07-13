package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-delivery-spec/cli/internal/profiles"
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

var initProfile string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold ODS configuration for a project",
	Long: `Initialize ODS in your repository with a single command.

Scaffolds:
  • .github/workflows/ods-ai-quality.yml  — CI workflow for AI code quality checks
  • .ods/policy.rego                       — an OPA Rego policy from a chosen profile

Policy profiles (--profile) are ready-made starting points:
  ods-way   (default) block critical issues, surface the rest, route by risk
  strict    also block high-severity issues and low-coverage AI code
  advisory  never blocks — warns and routes only (for incremental adoption)

Examples:
  ods init                       # scaffold with the recommended ods-way profile
  ods init --profile strict      # start from the strict profile
  ods init --profile advisory    # non-blocking, surface-only`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&initProfile, "profile", profiles.Default,
		fmt.Sprintf("policy profile to scaffold (%s)", strings.Join(profiles.Names(), ", ")))
}

func runInit(cmd *cobra.Command, args []string) error {
	profile, err := profiles.Get(initProfile)
	if err != nil {
		return err
	}

	workflowsDir := filepath.Join(".github", "workflows")
	odsDir := ".ods"

	for _, d := range []string{".github", workflowsDir, odsDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(workflowsDir, "ods-ai-quality.yml"): odsWorkflow,
		filepath.Join(odsDir, "policy.rego"):              profile.Policy,
	}

	fmt.Printf("Using policy profile: %s — %s\n", profile.Name, profile.Summary)

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
	fmt.Println("  1. Edit .ods/policy.rego to add custom enforcement rules")
	fmt.Println("  2. Install git hooks:  ods hook install")
	fmt.Println("  3. Commit and push — ODS will run on your next PR")

	return nil
}
