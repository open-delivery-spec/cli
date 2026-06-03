package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Install and manage git hooks",
	Long: `Install ODS git hooks to validate compliance before commits and pushes.

Supported hooks:
  install           Install all ODS hooks (pre-commit, commit-msg, pre-push)
  install pre-commit  Install only the pre-commit hook

Examples:
  ods hook install                      # Install all hooks
  ods hook install pre-commit           # Install pre-commit only
  ods hook install --hooks-dir .githooks  # Custom hooks directory`,
}

var (
	hookTarget string
	hooksDir   string
)

var hookInstallCmd = &cobra.Command{
	Use:   "install [hook-name]",
	Short: "Install ODS git hooks",
	Long: `Install ODS git hooks in your repository to catch compliance issues locally.

Installed hooks:
  pre-commit  — Validates branch naming before committing
  commit-msg  — Validates commit message format when writing commit
  pre-push    — Runs ods report before pushing to remote

This is the fastest way to get ODS compliance feedback — issues are caught
immediately in your terminal rather than waiting for CI to run.

Examples:
  ods hook install               # Install all hooks
  ods hook install pre-commit    # Install pre-commit only`,
	Args: cobra.MaximumNArgs(1),
	RunE: runHookInstall,
}

func init() {
	rootCmd.AddCommand(hookCmd)
	hookCmd.AddCommand(hookInstallCmd)
	hookInstallCmd.Flags().StringVar(&hooksDir, "hooks-dir", "", "custom git hooks directory (default: .git/hooks)")
}

func runHookInstall(cmd *cobra.Command, args []string) error {
	target := "all"
	if len(args) > 0 {
		target = args[0]
	}

	// Determine hooks directory
	gitDir := hooksDir
	if gitDir == "" {
		// Find .git directory
		out, err := exec.Command("git", "rev-parse", "--git-dir").Output()
		if err != nil {
			return fmt.Errorf("not a git repository (or git not found): %w", err)
		}
		gitDir = filepath.Join(strings.TrimSpace(string(out)), "hooks")
	}

	// Detect ODS CLI path
	odsPath, err := exec.LookPath("ods")
	if err != nil {
		// Fall back to go run if ods not in PATH
		odsPath = "ods"
	}

	hooks := map[string]string{
		"pre-commit": preCommitHook(odsPath),
		"commit-msg": commitMsgHook(odsPath),
		"pre-push":   prePushHook(odsPath),
	}

	installed := 0
	for name, script := range hooks {
		if target != "all" && target != name {
			continue
		}

		hookPath := filepath.Join(gitDir, name)

		// Check if hook already exists
		if existing, err := os.ReadFile(hookPath); err == nil {
			if strings.Contains(string(existing), "ODS") || strings.Contains(string(existing), "ods ") {
				fmt.Printf("  ⏭️  %s (already installed)\n", name)
				continue
			}
			// Back up existing hook
			backupPath := hookPath + ".ods-backup"
			if err := os.WriteFile(backupPath, existing, 0755); err != nil {
				return fmt.Errorf("backing up existing hook %s: %w", name, err)
			}
			fmt.Printf("  📦 Backed up existing %s → %s.ods-backup\n", name, name)

			// Append ODS check to existing hook
			script = string(existing) + "\n# === ODS compliance check (added by ods hook install) ===\n" + script
		}

		// Write hook
		if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
			return fmt.Errorf("writing hook %s: %w", name, err)
		}
		fmt.Printf("  ✅ Installed %s\n", name)
		installed++
	}

	if installed == 0 {
		fmt.Println("All ODS hooks are already installed.")
	} else {
		fmt.Printf("\n✅ Installed %d ODS hook(s) in %s\n", installed, gitDir)
		fmt.Println()
		fmt.Println("Hooks are now active. They will validate:")
		fmt.Println("  • Branch naming before commits")
		fmt.Println("  • Commit message format when committing")
		fmt.Println("  • ODS compliance report before pushing")
	}

	return nil
}

func preCommitHook(odsPath string) string {
	shebang := "#!/bin/sh"
	if runtime.GOOS == "windows" {
		shebang = "#!/bin/sh"
	}

	return fmt.Sprintf(`%s
# ODS pre-commit hook — validates branch naming
# Installed by: ods hook install

BRANCH=$(git branch --show-current)
if [ -z "$BRANCH" ]; then
    exit 0
fi

echo "🔍 ODS: Validating branch name '$BRANCH'..."
if ! %s validate branch "$BRANCH" 2>/dev/null; then
    echo ""
    echo "❌ ODS: Branch name '$BRANCH' is non-conformant."
    echo "   Branch names must follow: <type>/<description>"
    echo "   Valid types: feature, bugfix, hotfix, release, chore"
    echo ""
    echo "   Fix: rename your branch with 'git branch -m <new-name>'"
    echo "   Or generate a valid name: %s generate branch --type feature --description 'my-feature'"
    exit 1
fi
echo "✅ ODS: Branch name is conformant"
`, shebang, odsPath, odsPath)
}

func commitMsgHook(odsPath string) string {
	shebang := "#!/bin/sh"
	if runtime.GOOS == "windows" {
		shebang = "#!/bin/sh"
	}

	return fmt.Sprintf(`%s
# ODS commit-msg hook — validates commit message format
# Installed by: ods hook install

COMMIT_MSG_FILE="$1"
if [ ! -f "$COMMIT_MSG_FILE" ]; then
    exit 0
fi

# Skip merges, rebases, and amend commits
if grep -q '^Merge' "$COMMIT_MSG_FILE" 2>/dev/null; then
    exit 0
fi

echo "🔍 ODS: Validating commit message..."
if ! %s validate commit --file "$COMMIT_MSG_FILE" 2>/dev/null; then
    echo ""
    echo "❌ ODS: Commit message is non-conformant."
    echo "   Commit messages must follow Conventional Commits: <type>[scope]: <description>"
    echo "   Valid types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert"
    echo ""
    echo "   Fix: edit your commit message with 'git commit --amend'"
    echo "   Or generate one: %s generate commit --type feat --description 'add feature'"
    exit 1
fi
echo "✅ ODS: Commit message is conformant"
`, shebang, odsPath, odsPath)
}

func prePushHook(odsPath string) string {
	shebang := "#!/bin/sh"
	if runtime.GOOS == "windows" {
		shebang = "#!/bin/sh"
	}

	return fmt.Sprintf(`%s
# ODS pre-push hook — quick compliance check before pushing
# Installed by: ods hook install

echo "🔍 ODS: Running compliance check before push..."

# Quick check only — full report is for CI
FAILED=0

# 1. Validate branch name
BRANCH=$(git branch --show-current)
if [ -n "$BRANCH" ]; then
    if ! %s validate branch "$BRANCH" 2>/dev/null; then
        FAILED=1
    fi
fi

# 2. Validate latest commit message
COMMIT_MSG=$(git log -1 --format=%%B)
if [ -n "$COMMIT_MSG" ]; then
    echo "$COMMIT_MSG" | %s validate commit --stdin 2>/dev/null || FAILED=1
fi

if [ $FAILED -eq 1 ]; then
    echo ""
    echo "⚠️  ODS: Some checks failed. Use 'ods report' for a full report."
    echo "   To bypass (not recommended): git push --no-verify"
    exit 1
fi

echo "✅ ODS: Pre-push checks passed"
`, shebang, odsPath, odsPath)
}
