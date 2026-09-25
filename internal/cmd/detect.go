package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/spf13/cobra"
)

var (
	detectPRBody   string
	detectPRFile   string
	detectBranch   string
	detectDiffBase string
	detectCommits  int
	detectJSON     bool
	detectFormat   string
)

var detectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Detect AI-generated code in a change",
	Long: `Detect whether a git change contains AI-generated code by analyzing
commit trailers, branch names, PR descriptions, and code diff patterns.

Detection uses multiple independent signal sources and outputs a
confidence score — without requiring developer self-disclosure.

Signal sources (in order of confidence):
  1. git-ai authorship notes (refs/notes/ai), when present
  2. Commit trailers (Co-Authored-By: <ai-tool>, Assisted-by: AGENT:MODEL)
  3. PR description AI disclosure section
  4. Branch name prefix (claude/, copilot/, cursor/, codeium/, ai-)
  5. Code diff heuristics (comment ratio, naming patterns, error handling),
     only when nothing attests the change

Exit status is 0 whenever detection ran, whether or not AI was found; it is
non-zero only when detection itself failed. Use "ods check" to gate merges.

Examples:
  ods detect                                    # detect in HEAD~1..HEAD
  ods detect --diff-base origin/main            # detect against main branch
  ods detect --branch feature/ai-auth           # explicit branch name
  ods detect --pr-body "## AI Disclosure..."    # explicit PR body
  ods detect --commits 5 --json                 # last 5 commits, JSON output
  ods detect --diff-base HEAD~3 --format detail # detailed output`,
	RunE: runDetect,
}

func init() {
	rootCmd.AddCommand(detectCmd)

	detectCmd.Flags().StringVar(&detectDiffBase, "diff-base", "",
		"git ref to diff against (default: $ODS_DIFF_BASE or HEAD~1)")
	detectCmd.Flags().StringVar(&detectPRBody, "pr-body", "",
		"PR description body text")
	detectCmd.Flags().StringVar(&detectPRFile, "pr-file", "",
		"file containing PR description body")
	detectCmd.Flags().StringVar(&detectBranch, "branch", "",
		"branch name (auto-detected if not provided)")
	detectCmd.Flags().IntVar(&detectCommits, "commits", 10,
		"maximum number of commits to scan for AI markers")
	detectCmd.Flags().BoolVar(&detectJSON, "json", false,
		"output as JSON")
	detectCmd.Flags().StringVar(&detectFormat, "format", "summary",
		"output format: summary, detail, json")
}

func runDetect(cmd *cobra.Command, args []string) error {
	opts := detector.Options{
		DiffBase:   resolveDiffBase(detectDiffBase),
		MaxCommits: detectCommits,
	}

	prBody, err := resolvePRBody(detectPRBody, detectPRFile)
	if err != nil {
		return err
	}
	opts.PRBody = prBody
	opts.BranchName = resolveBranch(detectBranch)

	logx.Debugf("detect: diff base=%s branch=%q max commits=%d", opts.DiffBase, opts.BranchName, opts.MaxCommits)

	result, err := detector.Detect(opts)
	if err != nil {
		return fmt.Errorf("detection failed: %w", err)
	}

	logx.Debugf("detect: ai_generated=%t confidence=%.2f sources=%v", result.AIGenerated, result.Confidence, result.Sources)
	for _, ev := range result.Evidence {
		logx.Debugf("detect: evidence [%s] %s (%.2f)", ev.Source, ev.Value, ev.Confidence)
	}

	switch {
	case detectJSON || detectFormat == "json":
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling result: %w", err)
		}
		cmd.OutOrStdout().Write(data)
		cmd.OutOrStdout().Write([]byte("\n"))
	case detectFormat == "detail":
		printDetailed(cmd, result)
	default:
		printSummary(cmd, result)
	}

	// Detection ran, so whatever it found is a result, not an error: exit 0
	// whether or not AI was detected. Deciding what a positive detection
	// means for the merge is the policy's job, in `ods check`.
	return nil
}

func printSummary(cmd *cobra.Command, result *detector.DetectionResult) {
	if result.AIGenerated {
		fmt.Fprintf(cmd.OutOrStdout(), "\U0001f916  %s\n", result.Summary)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "\U0001f464  %s\n", result.Summary)
	}

	if len(result.Sources) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "   Sources: %s\n", strings.Join(result.Sources, ", "))
	}

	if len(result.Evidence) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "   Evidence:\n")
		for _, ev := range result.Evidence {
			fmt.Fprintf(cmd.OutOrStdout(), "     • [%s] %s (%.0f%%)\n",
				ev.Source, ev.Value, ev.Confidence*100)
		}
	}
}

func printDetailed(cmd *cobra.Command, result *detector.DetectionResult) {
	statusIcon := "\U0001f464"
	if result.AIGenerated {
		statusIcon = "\U0001f916"
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s  AI Code Detection Report\n", statusIcon)
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	fmt.Fprintf(cmd.OutOrStdout(), "Verdict:    %s\n", result.Summary)
	fmt.Fprintf(cmd.OutOrStdout(), "Confidence: %.0f%%\n", result.Confidence*100)
	fmt.Fprintf(cmd.OutOrStdout(), "Sources:    %s\n", strings.Join(result.Sources, ", "))
	fmt.Fprintln(cmd.OutOrStdout())

	if len(result.Evidence) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Signal Evidence:")
		for _, ev := range result.Evidence {
			fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s (confidence: %.0f%%)\n",
				ev.Source, ev.Value, ev.Confidence*100)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	if len(result.Files) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "File Analysis:")
		fmt.Fprintf(cmd.OutOrStdout(), "  %-40s %8s %8s %10s\n", "File", "AI Lines", "Total", "Confidence")
		fmt.Fprintln(cmd.OutOrStdout(), "  "+strings.Repeat("─", 70))
		for _, f := range result.Files {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-40s %8d %8d %9.0f%%\n",
				f.Path, f.AILines, f.TotalLines, f.Confidence*100)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	risk := "Low"
	if result.Confidence >= 0.8 {
		risk = "High"
	} else if result.Confidence >= 0.5 {
		risk = "Medium"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Risk Level: %s\n", risk)
}

// resolvePRBody returns the pull request description from --pr-body, then
// --pr-file, then ODS_PR_BODY. Every command that attributes a change reads
// it the same way, so a ticked disclosure box reaches the gate, not only
// `ods detect`.
func resolvePRBody(body, file string) (string, error) {
	if body != "" {
		return body, nil
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("reading PR file %s: %w", file, err)
		}
		return string(data), nil
	}
	return readEnvStr("ODS_PR_BODY"), nil
}

// resolveBranch returns the branch name from --branch, then ODS_BRANCH (set
// by validate-action), ODS_BRANCH_NAME, GITHUB_HEAD_REF, and finally the
// checked-out branch.
func resolveBranch(flag string) string {
	if flag != "" {
		return flag
	}
	for _, name := range []string{"ODS_BRANCH", "ODS_BRANCH_NAME", "GITHUB_HEAD_REF"} {
		if v := readEnvStr(name); v != "" {
			return v
		}
	}
	if out, err := gitOut("branch", "--show-current"); err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

func readEnvStr(name string) string {
	return os.Getenv(name)
}

func gitOut(args ...string) (string, error) {
	c := exec.Command("git", args...)
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
