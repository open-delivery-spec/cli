package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-delivery-spec/cli/internal/report"
	"github.com/spf13/cobra"
)

var (
	fixOutputDir string
	fixApply     bool
	fixDryRun    bool
)

// fixItem holds a single fix suggestion.
type fixItem struct {
	CheckID   string
	CheckName string
	Title     string
	Desc      string
	Template  string
}

var fixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Generate and apply fix suggestions for ODS compliance issues",
	Long: `Analyze the current repository and generate fix suggestions for all
compliance issues found. Fix suggestions are based on the ODS report.

Modes:
  ods fix                  Show fix suggestions in terminal
  ods fix --output dir     Write fix templates to a directory
  ods fix --apply          Auto-create fix templates in the repository
  ods fix --dry-run        Preview what would be created without making changes

Examples:
  ods fix                          # Show all fix suggestions
  ods fix --output ods-fixes       # Write templates to ods-fixes/
  ods fix --apply --dry-run        # Preview auto-apply actions
  ods fix --apply                  # Auto-create templates`,
	RunE: runFix,
}

func init() {
	rootCmd.AddCommand(fixCmd)
	fixCmd.Flags().StringVar(&fixOutputDir, "output", "", "directory to write fix templates")
	fixCmd.Flags().BoolVar(&fixApply, "apply", false, "auto-apply fixes by creating template files")
	fixCmd.Flags().BoolVar(&fixDryRun, "dry-run", false, "preview changes without making them")
}

func runFix(cmd *cobra.Command, args []string) error {
	inputs := report.DiscoverInputs()
	r := report.Build(inputs, report.Options{})

	var fixes []fixItem
	hasIssues := false

	for _, check := range r.Checks {
		if len(check.FixSuggestions) == 0 {
			continue
		}
		if check.Status != report.CheckFail && check.Status != report.CheckWarning {
			continue
		}
		hasIssues = true
		for _, fs := range check.FixSuggestions {
			fixes = append(fixes, fixItem{
				CheckID:   check.ID,
				CheckName: check.Name,
				Title:     fs.Title,
				Desc:      fs.Description,
				Template:  fs.Template,
			})
		}
	}

	if !hasIssues {
		fmt.Println("✅ No issues found — nothing to fix.")
		return nil
	}

	if fixOutputDir != "" {
		return writeFixesToDir(fixes, fixOutputDir)
	}

	if fixApply {
		return applyFixes(fixes, fixDryRun)
	}

	return printFixes(r, fixes)
}

func printFixes(r report.Report, fixes []fixItem) error {
	fmt.Printf("🔧 ODS Fix Suggestions — %d issue(s) found\n", len(fixes))
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("Current Score: %d / 100\n\n", r.Score)

	for i, fix := range fixes {
		fmt.Printf("%d. [%s] %s\n", i+1, fix.CheckName, fix.Title)
		fmt.Printf("   %s\n", fix.Desc)
		if fix.Template != "" {
			fmt.Println("   Template:")
			for _, line := range strings.Split(fix.Template, "\n") {
				fmt.Printf("     %s\n", line)
			}
		}
		fmt.Println()
	}

	fmt.Println("── Next steps ──")
	fmt.Println("  • Review the suggestions above")
	fmt.Println("  • Run 'ods fix --output ods-fixes' to save templates to files")
	fmt.Println("  • Run 'ods fix --apply' to auto-create fix files in your repo")
	fmt.Println("  • Use 'ods fix --dry-run' to preview changes before applying")
	return nil
}

func writeFixesToDir(fixes []fixItem, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	fmt.Printf("Writing %d fix templates to %s/\n", len(fixes), dir)
	for i, fix := range fixes {
		filename := filepath.Join(dir, fmt.Sprintf("%02d-%s.md", i+1, fix.CheckID))
		content := fmt.Sprintf("# %s: %s\n\n", fix.CheckName, fix.Title)
		content += fmt.Sprintf("## Description\n\n%s\n\n", fix.Desc)
		if fix.Template != "" {
			content += "## Template\n\n```\n" + fix.Template + "\n```\n"
		}
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
		fmt.Printf("  ✅ %s\n", filename)
	}
	return nil
}

func applyFixes(fixes []fixItem, dryRun bool) error {
	applied := 0
	skipped := 0

	for _, fix := range fixes {
		targetFile := determineTargetFile(fix.CheckID)
		if targetFile == "" {
			skipped++
			continue
		}

		if _, err := os.Stat(targetFile); err == nil {
			if dryRun {
				fmt.Printf("  ⏭️  Would skip (exists): %s\n", targetFile)
			} else {
				fmt.Printf("  ⏭️  Skipped (already exists): %s\n", targetFile)
			}
			skipped++
			continue
		}

		if dryRun {
			fmt.Printf("  📝 Would create: %s\n", targetFile)
			applied++
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetFile), 0755); err != nil {
			fmt.Printf("  ❌ Failed to create directory for %s: %v\n", targetFile, err)
			skipped++
			continue
		}
		if err := os.WriteFile(targetFile, []byte(fix.Template), 0644); err != nil {
			fmt.Printf("  ❌ Failed to create %s: %v\n", targetFile, err)
			skipped++
			continue
		}
		fmt.Printf("  ✅ Created: %s\n", targetFile)
		applied++
	}

	if dryRun {
		fmt.Printf("\nDry run: would apply %d fix(es), skip %d existing\n", applied, skipped)
	} else {
		fmt.Printf("\nApplied %d fix(es), skipped %d existing\n", applied, skipped)
	}
	return nil
}

func determineTargetFile(checkID string) string {
	switch checkID {
	case "ai-disclosure":
		return ".github/PULL_REQUEST_TEMPLATE.md"
	case "required-ci":
		return ".github/workflows/ods-ci.yml"
	case "approval-policy":
		return "CODEOWNERS"
	case "security-scan-evidence":
		return ".github/workflows/ods-security-scan.yml"
	case "commit-message":
		return ".gitmessage"
	case "pr-description":
		return ".github/PULL_REQUEST_TEMPLATE.md"
	case "release-readiness":
		return ".github/workflows/ods-release.yml"
	default:
		return ""
	}
}
