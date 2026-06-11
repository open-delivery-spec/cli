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
	Long: `Install ODS git hooks to validate compliance and detect AI code.

Supported hooks:
  install           Install all ODS hooks
  install pre-commit  Install only the pre-commit hook

Examples:
  ods hook install                      # Install all hooks
  ods hook install pre-commit           # Install pre-commit only`,
}

var (
	hookTarget string
	hooksDir   string
)

var hookInstallCmd = &cobra.Command{
	Use:   "install [hook-name]",
	Short: "Install ODS git hooks",
	Long: `Install ODS git hooks for local AI code detection and quality checks.

Installed hooks:
  pre-commit         — Detect AI code + validate branch naming
  commit-msg          — Validate commit message format
  prepare-commit-msg  — Auto-detect AI tools and add AI-assisted footer
  pre-push            — Score tech debt before pushing

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

	gitDir := hooksDir
	if gitDir == "" {
		out, err := exec.Command("git", "rev-parse", "--git-dir").Output()
		if err != nil {
			return fmt.Errorf("not a git repository (or git not found): %w", err)
		}
		gitDir = filepath.Join(strings.TrimSpace(string(out)), "hooks")
	}

	odsPath, err := exec.LookPath("ods")
	if err != nil {
		odsPath = "ods"
	}

	hooks := map[string]string{
		"pre-commit":         preCommitHook(odsPath),
		"commit-msg":         commitMsgHook(odsPath),
		"prepare-commit-msg": prepareCommitMsgHook(odsPath),
		"pre-push":           prePushHook(odsPath),
	}

	installed := 0
	for name, script := range hooks {
		if target != "all" && target != name {
			continue
		}

		hookPath := filepath.Join(gitDir, name)

		if existing, err := os.ReadFile(hookPath); err == nil {
			if strings.Contains(string(existing), "ODS") || strings.Contains(string(existing), "ods ") {
				fmt.Printf("  ⏭️  %s (already installed)\n", name)
				continue
			}
			backupPath := hookPath + ".ods-backup"
			if err := os.WriteFile(backupPath, existing, 0755); err != nil {
				return fmt.Errorf("backing up existing hook %s: %w", name, err)
			}
			fmt.Printf("  📦 Backed up existing %s → %s.ods-backup\n", name, name)
			script = string(existing) + "\n# === ODS compliance check (added by ods hook install) ===\n" + script
		}

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
		fmt.Println("Hooks are now active:")
		fmt.Println("  • AI code detection before commits")
		fmt.Println("  • Branch naming validation")
		fmt.Println("  • Commit message format validation")
		fmt.Println("  • Auto AI-assisted footer for AI-generated commits")
		fmt.Println("  • Tech debt score before pushing")
	}

	return nil
}

func shebang() string {
	if runtime.GOOS == "windows" {
		return "#!/bin/sh"
	}
	return "#!/bin/sh"
}

func preCommitHook(odsPath string) string {
	return fmt.Sprintf(`%s
# ODS pre-commit hook — AI code detection + branch naming
# Installed by: ods hook install

BRANCH=$(git branch --show-current)
if [ -z "$BRANCH" ]; then
    exit 0
fi

echo "🔍 ODS: Checking for AI-generated code..."
if %s detect --diff-base HEAD 2>/dev/null | grep -q "AI code detected"; then
    echo "🤖  AI-generated code detected in this commit"
    echo "   Ensure you've reviewed all AI-generated changes"
fi

echo "🔍 ODS: Validating branch name '$BRANCH'..."
if ! %s validate branch "$BRANCH" 2>/dev/null; then
    echo ""
    echo "❌ ODS: Branch name '$BRANCH' is non-conformant."
    echo "   Valid types: feature, bugfix, hotfix, release, chore"
    exit 1
fi
echo "✅ ODS: Pre-commit checks passed"
`, shebang(), odsPath, odsPath)
}

func commitMsgHook(odsPath string) string {
	return fmt.Sprintf(`%s
# ODS commit-msg hook — validates commit message format
# Installed by: ods hook install

COMMIT_MSG_FILE="$1"
if [ ! -f "$COMMIT_MSG_FILE" ]; then
    exit 0
fi

if grep -q '^Merge' "$COMMIT_MSG_FILE" 2>/dev/null; then
    exit 0
fi

echo "🔍 ODS: Validating commit message..."
if ! %s validate commit --file "$COMMIT_MSG_FILE" 2>/dev/null; then
    echo ""
    echo "❌ ODS: Commit message is non-conformant."
    echo "   Format: <type>[scope]: <description>"
    exit 1
fi
echo "✅ ODS: Commit message is conformant"
`, shebang(), odsPath)
}

func prepareCommitMsgHook(odsPath string) string {
	return fmt.Sprintf(`%s
# ODS prepare-commit-msg hook — auto-detect AI and add footer
# Installed by: ods hook install

COMMIT_MSG_FILE="$1"
COMMIT_SOURCE="$2"

# Only add AI footer for new commits (not merges, amends, etc.)
case "$COMMIT_SOURCE" in
    merge|commit) exit 0 ;;
esac

# Check if AI coding tools are active
AI_TOOL=""
if [ -n "$GITHUB_COPILOT" ] || [ -n "$COPILOT" ]; then
    AI_TOOL="GitHub Copilot"
elif [ -n "$CURSOR" ]; then
    AI_TOOL="Cursor"
elif [ -n "$CLAUDE_CODE" ]; then
    AI_TOOL="Claude Code"
fi

# Check if any staged files look AI-generated
if [ -z "$AI_TOOL" ]; then
    # Quick heuristic: check for AI branch prefix
    BRANCH=$(git branch --show-current 2>/dev/null)
    if echo "$BRANCH" | grep -qi "^ai-\|/ai-"; then
        AI_TOOL="AI Assistant"
    fi
fi

if [ -n "$AI_TOOL" ]; then
    # Only add if not already present
    if ! grep -qi "AI-assisted:" "$COMMIT_MSG_FILE" 2>/dev/null; then
        echo "" >> "$COMMIT_MSG_FILE"
        echo "AI-assisted: true" >> "$COMMIT_MSG_FILE"
        echo "AI-tool: $AI_TOOL" >> "$COMMIT_MSG_FILE"
        echo "🤖 ODS: Added AI disclosure footer (tool: $AI_TOOL)"
    fi
fi
`, shebang())
}

func prePushHook(odsPath string) string {
	return fmt.Sprintf(`%s
# ODS pre-push hook — tech debt scoring before push
# Installed by: ods hook install

echo "🔍 ODS: Scoring technical debt impact..."

if %s score 2>/dev/null | grep -q "increase"; then
    echo ""
    echo "⚠️  ODS: This push may increase technical debt."
    echo "   Run 'ods score --format detail' for a full breakdown."
    echo "   To bypass: git push --no-verify"
fi

echo "✅ ODS: Pre-push check complete"
`, shebang(), odsPath)
}
