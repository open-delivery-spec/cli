package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/report"
)

// ─── detect ──────────────────────────────────────────────────────

// resetDetectFlags restores the detect command's flag state.
func resetDetectFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		detectPRBody, detectPRFile, detectBranch, detectDiffBase = "", "", "", ""
		detectCommits, detectJSON, detectFormat = 10, false, "summary"
	})
}

// TestRunDetect covers every output format over an AI-attributed commit,
// and a positive detection exiting cleanly: the gate is check's job.
func TestRunDetect(t *testing.T) {
	resetDetectFlags(t)
	gateRepo(t)

	detectJSON = true
	c, buf := bufCmd()
	if err := runDetect(c, nil); err != nil {
		t.Fatalf("a positive detection must not be an error: %v", err)
	}
	var res detector.DetectionResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("detect --json output: %v\n%s", err, buf)
	}
	if !res.AIGenerated || !contains(res.Sources, "commit-trailer") {
		t.Errorf("result = %+v, want AI from the commit trailer", res)
	}

	detectJSON, detectFormat = false, "detail"
	c, buf = bufCmd()
	if err := runDetect(c, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AI Code Detection Report", "File Analysis:", "svc.go", "Risk Level: High"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("detail output missing %q:\n%s", want, buf)
		}
	}

	detectFormat = "summary"
	c, buf = bufCmd()
	if err := runDetect(c, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Sources: commit-trailer") {
		t.Errorf("summary output missing the sources:\n%s", buf)
	}
}

// TestRunDetect_PRFile: --pr-file feeds the PR-body source, and an unreadable
// file is an error rather than a silent "no disclosure".
func TestRunDetect_PRFile(t *testing.T) {
	resetDetectFlags(t)
	clearPipelineEnv(t)
	dir := t.TempDir()
	t.Chdir(dir)
	detectBranch = "feature/x"
	detectPRFile = filepath.Join(dir, "body.md")
	mustWrite(t, detectPRFile, "## AI Disclosure\n- [x] This PR contains AI-generated code\n")
	detectJSON = true

	c, buf := bufCmd()
	if err := runDetect(c, nil); err != nil {
		t.Fatal(err)
	}
	var res detector.DetectionResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !contains(res.Sources, "pr-body") {
		t.Errorf("sources = %v, want pr-body from --pr-file", res.Sources)
	}

	detectPRFile = filepath.Join(dir, "missing.md")
	c, _ = bufCmd()
	if err := runDetect(c, nil); err == nil || !strings.Contains(err.Error(), "reading PR file") {
		t.Fatalf("err = %v, want a reading-PR-file error", err)
	}
}

// TestPrintDetect_HumanAndRiskBands: a human verdict gets the person icon,
// and the detail risk level follows the confidence bands.
func TestPrintDetect_HumanAndRiskBands(t *testing.T) {
	c, buf := bufCmd()
	printSummary(c, &detector.DetectionResult{Summary: "No AI code detected"})
	if !strings.HasPrefix(buf.String(), "\U0001f464") {
		t.Errorf("human summary should use the person icon: %q", buf)
	}

	for conf, want := range map[float64]string{0.6: "Risk Level: Medium", 0.2: "Risk Level: Low"} {
		c, buf := bufCmd()
		printDetailed(c, &detector.DetectionResult{Confidence: conf})
		if !strings.Contains(buf.String(), want) {
			t.Errorf("confidence %.1f: detail missing %q:\n%s", conf, want, buf)
		}
	}
}

// TestResolvePRBodyAndBranchFallbacks: the PR body falls back to ODS_PR_BODY,
// and the branch to the checked-out branch when no flag or variable names it.
func TestResolvePRBodyAndBranchFallbacks(t *testing.T) {
	clearPipelineEnv(t)
	t.Setenv("ODS_PR_BODY", "from the environment")
	if got, err := resolvePRBody("", ""); err != nil || got != "from the environment" {
		t.Errorf("resolvePRBody = %q, %v; want the ODS_PR_BODY value", got, err)
	}
	if got, err := resolvePRBody("from the flag", "ignored.md"); err != nil || got != "from the flag" {
		t.Errorf("resolvePRBody = %q, %v; want --pr-body to win over --pr-file and the environment", got, err)
	}

	newRepo(t)
	if got := resolveBranch(""); got != "main" {
		t.Errorf("resolveBranch = %q, want the checked-out branch main", got)
	}
	t.Chdir(t.TempDir())
	if got := resolveBranch(""); got != "" {
		t.Errorf("resolveBranch outside a repository = %q, want empty", got)
	}
}

// ─── init ────────────────────────────────────────────────────────

// TestRunInit scaffolds the workflow and the default policy, and leaves files
// that already exist untouched on a second run.
func TestRunInit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := captureStdout(t, func() {
		if err := runInit(initCmd, nil); err != nil {
			t.Fatalf("runInit: %v", err)
		}
	})
	if !strings.Contains(out, "Created: .github/workflows/ods-ai-quality.yml") || !strings.Contains(out, "ODS initialized") {
		t.Errorf("first run output:\n%s", out)
	}
	wf, err := os.ReadFile(filepath.Join(dir, ".github", "workflows", "ods-ai-quality.yml"))
	if err != nil || !strings.Contains(string(wf), "open-delivery-spec/validate-action@v1") {
		t.Errorf("workflow not scaffolded: %v\n%s", err, wf)
	}
	pol, err := os.ReadFile(filepath.Join(dir, ".ods", "policy.rego"))
	if err != nil || !strings.HasPrefix(string(pol), "package ods.policy") {
		t.Errorf("policy not scaffolded: %v", err)
	}

	mustWrite(t, filepath.Join(dir, ".ods", "policy.rego"), "package ods.policy\n# edited\n")
	out = captureStdout(t, func() {
		if err := runInit(initCmd, nil); err != nil {
			t.Fatalf("second runInit: %v", err)
		}
	})
	if strings.Count(out, "Skipped (already exists)") != 2 {
		t.Errorf("second run should skip both files:\n%s", out)
	}
	if pol, _ := os.ReadFile(filepath.Join(dir, ".ods", "policy.rego")); !strings.Contains(string(pol), "# edited") {
		t.Error("an existing policy was overwritten")
	}
}

// TestRunInit_Failures: a .github that is a file, or an unwritable .ods, stops
// init with an error naming the path.
func TestRunInit_Failures(t *testing.T) {
	t.Run("directory blocked by a file", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		mustWrite(t, filepath.Join(dir, ".github"), "not a directory")
		var err error
		captureStdout(t, func() { err = runInit(initCmd, nil) })
		if err == nil || !strings.Contains(err.Error(), "creating directory") {
			t.Fatalf("err = %v, want a creating-directory error", err)
		}
	})

	t.Run("unwritable directory", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs POSIX permissions and a non-root user")
		}
		dir := t.TempDir()
		t.Chdir(dir)
		for _, d := range []string{filepath.Join(dir, ".github", "workflows"), filepath.Join(dir, ".ods")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(d, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(d, 0o755) })
		}
		var err error
		captureStdout(t, func() { err = runInit(initCmd, nil) })
		if err == nil || !strings.Contains(err.Error(), "writing") {
			t.Fatalf("err = %v, want a writing error", err)
		}
	})
}

// ─── report ──────────────────────────────────────────────────────

// resetReportFlags restores the report and report merge flag state.
func resetReportFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		reportSince, reportJSON, reportHTML, reportMaxCmts, reportRepo = "90 days ago", false, "", 0, ""
		mergeJSON, mergeHTML, mergeMarkdown = false, "", ""
	})
}

// TestRunReport covers the attribution report over a fixture history in each
// output: JSON (with the --repo override), the HTML dashboard to a file and to
// stdout, and the text summary with its per-tool lines.
func TestRunReport(t *testing.T) {
	resetReportFlags(t)
	gateRepo(t)
	reportSince = "2000-01-01"

	reportJSON, reportRepo = true, "acme/widgets"
	c, buf := bufCmd()
	if err := runReport(c, nil); err != nil {
		t.Fatalf("runReport: %v", err)
	}
	var r report.Report
	if err := json.Unmarshal(buf.Bytes(), &r); err != nil {
		t.Fatalf("report --json: %v\n%s", err, buf)
	}
	if r.Repo != "acme/widgets" || r.TotalCommits != 2 || r.AICommits != 1 || r.ByTool["Claude"] != 1 {
		t.Errorf("report = %+v, want acme/widgets with 1 of 2 commits AI (Claude)", r)
	}

	reportJSON = false
	reportHTML = filepath.Join(t.TempDir(), "report.html")
	c, buf = bufCmd()
	if err := runReport(c, nil); err != nil {
		t.Fatal(err)
	}
	if html, err := os.ReadFile(reportHTML); err != nil || !strings.Contains(string(html), "<!DOCTYPE html>") {
		t.Errorf("HTML dashboard not written: %v", err)
	}
	if !strings.Contains(buf.String(), "Wrote AI attribution dashboard to") {
		t.Errorf("output = %q", buf)
	}

	reportHTML = "-"
	c, buf = bufCmd()
	if err := runReport(c, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "<!DOCTYPE html>") {
		t.Errorf("--html - should print the dashboard:\n%.200s", buf)
	}

	reportHTML = ""
	c, buf = bufCmd()
	if err := runReport(c, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ODS AI Attribution Report — since 2000-01-01", "2 total · 1 AI-assisted (50%)", "By tool:", "Claude"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("text report missing %q:\n%s", want, buf)
		}
	}
}

// TestRunReport_EmptyWindowAndFailures: an empty window says so, an HTML path
// that cannot be written is an error, and so is running outside git.
func TestRunReport_EmptyWindowAndFailures(t *testing.T) {
	resetReportFlags(t)
	gateRepo(t)

	reportSince = "2090-01-01" // git ignores years past 2099
	c, buf := bufCmd()
	if err := runReport(c, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No commits in the selected window.") {
		t.Errorf("empty window output:\n%s", buf)
	}

	reportSince = "2000-01-01"
	reportHTML = filepath.Join(t.TempDir(), "no-such-dir", "r.html")
	c, _ = bufCmd()
	if err := runReport(c, nil); err == nil || !strings.Contains(err.Error(), "writing HTML report") {
		t.Fatalf("err = %v, want an HTML write error", err)
	}

	reportHTML = ""
	t.Chdir(t.TempDir())
	c, _ = bufCmd()
	if err := runReport(c, nil); err == nil {
		t.Fatal("runReport outside a git repository returned nil")
	}
}

// writeReports writes two per-repository reports for merge tests; the second
// has no repo field, so the merge names it after its file.
func writeReports(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	mustWrite(t, a, `{"repo":"acme/a","since":"90 days ago","total_commits":4,"ai_commits":2,"human_commits":2,
	  "ai_commit_share":0.5,"total_changed_lines":40,"ai_changed_lines":20,"ai_line_share":0.5,"by_tool":{"Claude":2}}`)
	b := filepath.Join(dir, "beta.json")
	mustWrite(t, b, `{"since":"90 days ago","total_commits":2,"ai_commits":0,"human_commits":2,
	  "total_changed_lines":10,"by_tool":{}}`)
	return []string{a, b}
}

// TestRunReportMerge covers the organization merge in each output: JSON,
// HTML and Markdown to files or stdout, and the text summary.
func TestRunReportMerge(t *testing.T) {
	resetReportFlags(t)
	paths := writeReports(t)

	mergeJSON = true
	c, buf := bufCmd()
	if err := runReportMerge(c, paths); err != nil {
		t.Fatalf("runReportMerge: %v", err)
	}
	var o report.OrgReport
	if err := json.Unmarshal(buf.Bytes(), &o); err != nil {
		t.Fatalf("merge --json: %v\n%s", err, buf)
	}
	if o.ReposCovered != 2 || o.TotalCommits != 6 || o.AICommits != 2 {
		t.Errorf("org = %+v, want 2 repos, 2 of 6 commits AI", o)
	}
	var names []string
	for _, r := range o.Repos {
		names = append(names, r.Repo)
	}
	if !contains(names, "beta") || !contains(names, "acme/a") {
		t.Errorf("repos = %v, want acme/a and beta (named after its file)", names)
	}

	mergeJSON = false
	dir := t.TempDir()
	mergeHTML, mergeMarkdown = filepath.Join(dir, "org.html"), filepath.Join(dir, "org.md")
	c, buf = bufCmd()
	if err := runReportMerge(c, paths); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Wrote organization dashboard") || !strings.Contains(buf.String(), "Wrote organization summary") {
		t.Errorf("output = %q", buf)
	}
	if md, err := os.ReadFile(mergeMarkdown); err != nil || !strings.Contains(string(md), "| acme/a |") {
		t.Errorf("Markdown summary not written: %v", err)
	}

	mergeHTML, mergeMarkdown = "-", "-"
	c, buf = bufCmd()
	if err := runReportMerge(c, paths); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "<!DOCTYPE html>") || !strings.Contains(buf.String(), "## \U0001f4ca AI Attribution — organization") {
		t.Errorf("--html - --markdown - should print both documents:\n%.300s", buf)
	}

	mergeHTML, mergeMarkdown = "", ""
	c, buf = bufCmd()
	if err := runReportMerge(c, paths); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 scanned · 1 with AI-assisted commits", "By tool:", "By repository:", "acme/a"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("text summary missing %q:\n%s", want, buf)
		}
	}
}

// TestRunReportMerge_EmptyAndFailures: reports with no commits print the
// summary line, and unreadable, malformed or unwritable paths are errors.
func TestRunReportMerge_EmptyAndFailures(t *testing.T) {
	resetReportFlags(t)
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.json")
	mustWrite(t, empty, `{"repo":"acme/empty","since":"90 days ago","total_commits":0}`)
	c, buf := bufCmd()
	if err := runReportMerge(c, []string{empty}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "1 repositories, no commits in the selected window.") {
		t.Errorf("empty merge output:\n%s", buf)
	}

	broken := filepath.Join(dir, "broken.json")
	mustWrite(t, broken, `{"repo":`)
	for _, tc := range []struct {
		name  string
		paths []string
		set   func()
		want  string
	}{
		{"missing file", []string{filepath.Join(dir, "missing.json")}, func() {}, "reading"},
		{"malformed file", []string{broken}, func() {}, "parsing"},
		{"unwritable HTML", []string{empty}, func() { mergeHTML = filepath.Join(dir, "x", "o.html") }, "writing HTML report"},
		{"unwritable Markdown", []string{empty}, func() { mergeHTML, mergeMarkdown = "", filepath.Join(dir, "x", "o.md") }, "writing Markdown summary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.set()
			c, _ := bufCmd()
			if err := runReportMerge(c, tc.paths); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// ─── score ───────────────────────────────────────────────────────

// TestRunScore_Formats covers the detail and summary output of ods score, and
// the critical risk band's icon.
func TestRunScore_Formats(t *testing.T) {
	t.Cleanup(func() { scoreFormat, scoreJSON = "summary", false })
	clearPipelineEnv(t)
	t.Chdir(t.TempDir())

	scoreFormat = "detail"
	c, buf := bufCmd()
	if err := runScore(c, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Technical Debt Score Report") {
		t.Errorf("detail output:\n%s", buf)
	}

	scoreFormat = "summary"
	c, buf = bufCmd()
	if err := runScore(c, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Technical Debt Score\n") || !strings.Contains(buf.String(), "Verdict:") {
		t.Errorf("summary output:\n%s", buf)
	}

	if got := riskIcon("critical"); got != "❌" {
		t.Errorf("riskIcon(critical) = %q, want ❌", got)
	}
}

// ─── root ────────────────────────────────────────────────────────

// TestExecute covers the root command: --version prints the build info, and
// ODS_DEBUG or --debug turns on debug logging before a subcommand runs.
func TestExecute(t *testing.T) {
	logBuf := &bytes.Buffer{}
	logx.SetOutput(logBuf)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.Flags().Set("version", "false")
		rootCmd.PersistentFlags().Set("debug", "false")
		debugFlag, rulesJSON = false, false
		logx.SetEnabled(false)
		logx.SetOutput(os.Stderr)
	})
	out := &bytes.Buffer{}
	rootCmd.SetOut(out)
	rootCmd.SetErr(out)

	rootCmd.SetArgs([]string{"--version"})
	if err := Execute(); err != nil {
		t.Fatalf("ods --version: %v", err)
	}
	if !strings.HasPrefix(out.String(), "ods dev (commit unknown, built unknown)") {
		t.Errorf("version output = %q", out)
	}

	for name, args := range map[string][]string{
		"ODS_DEBUG": {"rules", "--json"},
		"--debug":   {"--debug", "rules", "--json"},
	} {
		t.Run(name, func(t *testing.T) {
			logx.SetEnabled(false)
			logBuf.Reset()
			if name == "ODS_DEBUG" {
				t.Setenv("ODS_DEBUG", "1")
			} else {
				t.Setenv("ODS_DEBUG", "")
			}
			rootCmd.SetArgs(args)
			if err := Execute(); err != nil {
				t.Fatalf("ods %v: %v", args, err)
			}
			logx.Debugf("probe")
			if !strings.Contains(logBuf.String(), "[ods:debug] probe") {
				t.Errorf("%s did not enable debug logging", name)
			}
		})
	}
}
