package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/sarif"
	"github.com/spf13/cobra"
)

var (
	analyzeFile   string
	analyzeDir    string
	analyzeAIOnly bool
	analyzeJSON   bool
	analyzeFormat string
	analyzeSARIF  string
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze AI-generated code for quality defects",
	Long: `Analyze code for known AI-generated quality defects including:
  ai-unsafe-deserialization   — json.Unmarshal into interface{} (high)
  ai-inconsistent-pattern     — Mixed naming conventions and indentation (medium)
  ai-redundant-error-handling — Dense clusters of if-err-nil blocks (info)
  ai-over-commenting          — Comment-to-code ratio >=40% (info)

The built-in heuristics are intentionally conservative: redundant-error-handling
and over-commenting are informational style hints only and never block a PR.
For authoritative, multi-language analysis, import findings from dedicated tools
via --sarif (see below).

Use --sarif to import findings from external tools (semgrep, CodeQL, etc.)
and merge them into the ODS issue list. The SARIF file's results are
converted to ODS severity levels and included in the JSON output.

Use --ai-only to focus analysis on files detected as AI-generated.

Examples:
  ods analyze --file auth.go
  ods analyze --dir ./src
  ods analyze --file handler.go --json
  ods analyze --dir ./pkg --ai-only
  ods analyze --sarif semgrep.sarif --json`,
	RunE: runAnalyze,
}

func init() {
	rootCmd.AddCommand(analyzeCmd)

	analyzeCmd.Flags().StringVarP(&analyzeFile, "file", "f", "",
		"file to analyze")
	analyzeCmd.Flags().StringVarP(&analyzeDir, "dir", "d", "",
		"directory to analyze (recursively)")
	analyzeCmd.Flags().BoolVar(&analyzeAIOnly, "ai-only", false,
		"only analyze files detected as AI-generated")
	analyzeCmd.Flags().BoolVar(&analyzeJSON, "json", false,
		"output as JSON")
	analyzeCmd.Flags().StringVar(&analyzeFormat, "format", "summary",
		"output format: summary, detail, json")
	analyzeCmd.Flags().StringVar(&analyzeSARIF, "sarif", "",
		"SARIF v2.1.0 file to merge into the analysis result")
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	var files map[string][]string

	if analyzeFile != "" {
		data, err := os.ReadFile(analyzeFile)
		if err != nil {
			return fmt.Errorf("reading file %s: %w", analyzeFile, err)
		}
		lines := strings.Split(string(data), "\n")
		files = map[string][]string{analyzeFile: lines}
	} else if analyzeDir != "" {
		var err error
		files, err = readDir(analyzeDir)
		if err != nil {
			return fmt.Errorf("reading directory %s: %w", analyzeDir, err)
		}
	} else {
		// Default: analyze git diff
		diffFiles, err := getGitDiffFiles("HEAD~1")
		if err == nil && len(diffFiles) > 0 {
			files = diffFiles
		} else if analyzeSARIF == "" {
			// No local code to analyze and no external findings to merge.
			return fmt.Errorf("no input provided: use --file, --dir, --sarif, or run in a git repo with changes")
		}
		// Otherwise: no diff code, but --sarif was given — proceed with no local
		// files and let the SARIF merge below supply the findings. This is the
		// multi-language path: a PR may touch no analyzable code yet still carry
		// authoritative findings from an external scanner.
	}

	// If --ai-only, filter to files detected as AI-generated
	if analyzeAIOnly {
		detectResult, err := detector.Detect(detector.Options{
			DiffBase:   "HEAD~1",
			MaxCommits: 10,
		})
		if err == nil && detectResult.AIGenerated {
			aiFiles := make(map[string]bool)
			for _, f := range detectResult.Files {
				aiFiles[f.Path] = true
			}
			filtered := make(map[string][]string)
			for path, lines := range files {
				if aiFiles[path] || detectResult.Confidence >= 0.5 {
					filtered[path] = lines
				}
			}
			if len(filtered) > 0 {
				files = filtered
			}
		}
	}

	opts := analyzer.Options{
		AIOnly: analyzeAIOnly,
		Files:  files,
	}

	logx.Debugf("analyze: scanning %d file(s)", len(files))
	result := analyzer.Analyze(opts)
	logx.Debugf("analyze: found %d issue(s) across %d lines", len(result.Issues), result.TotalLines)

	// Merge SARIF findings when --sarif is provided.
	if analyzeSARIF != "" {
		sarifIssues, err := sarif.Load(analyzeSARIF)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: loading SARIF file: %v\n", err)
		} else {
			logx.Debugf("analyze: merged %d SARIF finding(s) from %s", len(sarifIssues), analyzeSARIF)
			result.Issues = append(result.Issues, sarifIssues...)
			result.Summary = analyzer.ResummarizeSARIF(result.Issues)
		}
	}

	switch {
	case analyzeJSON || analyzeFormat == "json":
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling result: %w", err)
		}
		cmd.OutOrStdout().Write(data)
		cmd.OutOrStdout().Write([]byte("\n"))
	case analyzeFormat == "detail":
		printAnalyzeDetail(cmd, result)
	default:
		printAnalyzeSummary(cmd, result)
	}

	// Exit non-zero if critical issues found
	if result.HasCritical() {
		return fmt.Errorf("critical quality issues found: %d", result.CriticalCount())
	}
	return nil
}

func printAnalyzeSummary(cmd *cobra.Command, result *analyzer.AnalysisResult) {
	if len(result.Issues) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "✅  %s\n", result.Summary)
		return
	}

	counts := result.IssueCounts()
	icon := "⚠️"
	if result.HasCritical() {
		icon = "❌"
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", icon, result.Summary)
	fmt.Fprintf(cmd.OutOrStdout(), "   Density: %.1f issues/KLOC\n", result.IssueDensity())

	// Show top issues
	fmt.Fprintf(cmd.OutOrStdout(), "   Top issues:\n")
	shown := 0
	for _, issue := range result.Issues {
		if shown >= 5 {
			break
		}
		sevIcon := severityIcon(issue.Severity)
		fmt.Fprintf(cmd.OutOrStdout(), "     %s [%s] %s:%d — %s\n",
			sevIcon, issue.Rule, issue.File, issue.Line, issue.Message)
		shown++
	}
	if len(result.Issues) > 5 {
		fmt.Fprintf(cmd.OutOrStdout(), "     ... and %d more\n", len(result.Issues)-5)
	}

	// Show suggestion hint
	for _, issue := range result.Issues {
		if issue.Suggestion != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "\n💡  %s\n    → %s\n", issue.Rule, issue.Suggestion)
			break
		}
	}

	_ = counts
}

func printAnalyzeDetail(cmd *cobra.Command, result *analyzer.AnalysisResult) {
	counts := result.IssueCounts()

	statusIcon := "✅"
	if result.HasCritical() {
		statusIcon = "❌"
	} else if len(result.Issues) > 0 {
		statusIcon = "⚠️"
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s  AI Code Quality Analysis Report\n", statusIcon)
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	fmt.Fprintf(cmd.OutOrStdout(), "Summary:     %s\n", result.Summary)
	fmt.Fprintf(cmd.OutOrStdout(), "Total lines: %d\n", result.TotalLines)
	fmt.Fprintf(cmd.OutOrStdout(), "Issue density: %.1f/KLOC\n", result.IssueDensity())
	fmt.Fprintf(cmd.OutOrStdout(), "Severity:    C:%d H:%d M:%d L:%d I:%d\n",
		counts["critical"], counts["high"], counts["medium"], counts["low"], counts["info"])
	fmt.Fprintln(cmd.OutOrStdout())

	if len(result.Issues) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No quality issues detected.")
		return
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Issues:")
	fmt.Fprintf(cmd.OutOrStdout(), "  %-6s %-35s %-6s %s\n", "Severity", "Rule", "Line", "Message")
	fmt.Fprintln(cmd.OutOrStdout(), "  "+strings.Repeat("─", 100))
	for _, issue := range result.Issues {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s %-6s %-35s %-6d %s\n",
			severityIcon(issue.Severity), issue.Severity, issue.Rule, issue.Line, issue.Message)
	}
	fmt.Fprintln(cmd.OutOrStdout())

	// Suggestions
	fmt.Fprintln(cmd.OutOrStdout(), "Suggestions:")
	for _, issue := range result.Issues {
		if issue.Suggestion != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  [%s] → %s\n", issue.Rule, issue.Suggestion)
		}
	}
}

func severityIcon(severity string) string {
	switch severity {
	case "critical":
		return "🔴"
	case "high":
		return "🟠"
	case "medium":
		return "🟡"
	case "low":
		return "🔵"
	default:
		return "⚪"
	}
}

// getGitDiffFiles returns changed code files with their added lines.
// Only includes files with recognised code extensions that have added content.
func getGitDiffFiles(base string) (map[string][]string, error) {
	out, err := exec.Command("git", "diff", "--name-only", base).Output()
	if err != nil {
		return nil, err
	}

	files := make(map[string][]string)
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || !isCodeFileExt(name) {
			continue
		}
		// Get the diff for this file
		diffOut, err := exec.Command("git", "diff", base, "--", name).Output()
		if err != nil {
			continue
		}
		lines := extractAdded(diffOut)
		if len(lines) > 0 {
			files[name] = lines
		}
	}
	return files, nil
}

// getAllChangedFiles returns all file paths changed in the diff, with no extension
// filtering. Used to populate input.changed_files in Rego policy evaluation.
func getAllChangedFiles(base string) ([]string, error) {
	out, err := exec.Command("git", "diff", "--name-only", base).Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

func extractAdded(diff []byte) []string {
	var lines []string
	for _, line := range strings.Split(string(diff), "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "++") {
			lines = append(lines, line[1:])
		}
	}
	return lines
}

func isCodeFileExt(path string) bool {
	codeExts := map[string]bool{
		".go": true, ".rs": true, ".py": true, ".js": true, ".ts": true,
		".tsx": true, ".jsx": true, ".java": true, ".kt": true, ".swift": true,
		".c": true, ".cpp": true, ".h": true, ".hpp": true, ".cs": true,
		".rb": true, ".php": true, ".scala": true, ".clj": true, ".ex": true,
		".exs": true, ".elm": true, ".hs": true, ".ml": true, ".mli": true,
		".vue": true, ".svelte": true,
	}
	for ext := range codeExts {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

// readDir reads all code files in a directory recursively.
func readDir(dir string) (map[string][]string, error) {
	files := make(map[string][]string)
	err := walkDir(dir, func(path string, data []byte) {
		if isCodeFileExt(path) {
			files[path] = strings.Split(string(data), "\n")
		}
	})
	return files, err
}

func walkDir(dir string, fn func(string, []byte)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := dir + "/" + entry.Name()
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if err := walkDir(path, fn); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(path)
			if err == nil {
				fn(path, data)
			}
		}
	}
	return nil
}
