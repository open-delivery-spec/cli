package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/open-delivery-spec/cli/internal/analyzer"
)

// unsafeGo trips ai-unsafe-deserialization (high): json.Unmarshal into an
// interface{} declared a line above.
const unsafeGo = "package p\n\nfunc Load(b []byte) interface{} {\n\tvar data interface{}\n\tjson.Unmarshal(b, &data)\n\treturn data\n}\n"

// resetAnalyzeFlags restores the analyze command's package-level flag state,
// including the defaults cobra registered.
func resetAnalyzeFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		analyzeFile, analyzeDir, analyzeSARIF, analyzeDiffBase = "", "", "", ""
		analyzeJSON = false
		analyzeFormat, analyzeFailOn = "summary", "critical"
	})
}

func decodeAnalysis(t *testing.T, data []byte) analyzer.AnalysisResult {
	t.Helper()
	var res analyzer.AnalysisResult
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("analyze output is not valid JSON: %v\n%s", err, data)
	}
	return res
}

func hasIssue(res analyzer.AnalysisResult, rule, file string) bool {
	for _, iss := range res.Issues {
		if iss.Rule == rule && iss.File == file {
			return true
		}
	}
	return false
}

// TestIssuesAtLeast covers the --fail-on threshold: severities at or above
// it count, the name is case-insensitive, and an unknown threshold falls back
// to critical rather than failing on everything.
func TestIssuesAtLeast(t *testing.T) {
	res := &analyzer.AnalysisResult{Issues: []analyzer.Issue{
		{Severity: "info"}, {Severity: "medium"}, {Severity: "high"}, {Severity: "critical"},
	}}
	cases := []struct {
		threshold string
		want      int
	}{
		{"info", 4},
		{"medium", 3},
		{"HIGH", 2},
		{"critical", 1},
		{"bogus", 1},
	}
	for _, tc := range cases {
		if got := issuesAtLeast(res, tc.threshold); got != tc.want {
			t.Errorf("issuesAtLeast(%q) = %d, want %d", tc.threshold, got, tc.want)
		}
	}
}

// TestRunAnalyze_PositionalArgs covers the pre-commit hook path: file
// arguments are analyzed, non-code files are skipped, and --fail-on decides
// whether the findings fail the run.
func TestRunAnalyze_PositionalArgs(t *testing.T) {
	resetAnalyzeFlags(t)
	dir := t.TempDir()
	code := filepath.Join(dir, "load.go")
	mustWrite(t, code, unsafeGo)
	notes := filepath.Join(dir, "NOTES.md")
	mustWrite(t, notes, "# notes\n")
	analyzeJSON = true

	c, buf := bufCmd()
	if err := runAnalyze(c, []string{code, notes}); err != nil {
		t.Fatalf("a high finding must not fail under the default critical threshold: %v", err)
	}
	res := decodeAnalysis(t, buf.Bytes())
	if !hasIssue(res, "ai-unsafe-deserialization", code) {
		t.Errorf("issues = %+v, want ai-unsafe-deserialization in %s", res.Issues, code)
	}
	if want := len(strings.Split(unsafeGo, "\n")); res.TotalLines != want {
		t.Errorf("total_lines = %d, want %d: the Markdown file must be skipped", res.TotalLines, want)
	}

	t.Run("fail-on high fails the run", func(t *testing.T) {
		analyzeFailOn = "high"
		c, _ := bufCmd()
		err := runAnalyze(c, []string{code})
		if err == nil || !strings.Contains(err.Error(), `at or above severity "high"`) {
			t.Fatalf("err = %v, want a fail-on high error", err)
		}
		if !c.SilenceUsage {
			t.Error("a findings failure should not print usage")
		}
	})

	t.Run("unreadable argument", func(t *testing.T) {
		c, _ := bufCmd()
		err := runAnalyze(c, []string{filepath.Join(dir, "missing.go")})
		if err == nil || !strings.Contains(err.Error(), "reading file") {
			t.Fatalf("err = %v, want a reading-file error", err)
		}
	})
}

// TestRunAnalyze_FileFlag covers --file with the default summary output.
func TestRunAnalyze_FileFlag(t *testing.T) {
	resetAnalyzeFlags(t)
	dir := t.TempDir()
	analyzeFile = filepath.Join(dir, "load.go")
	mustWrite(t, analyzeFile, unsafeGo)

	c, buf := bufCmd()
	if err := runAnalyze(c, nil); err != nil {
		t.Fatalf("runAnalyze: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "ai-unsafe-deserialization") || !strings.Contains(out, "Top issues") {
		t.Errorf("summary output missing the finding:\n%s", out)
	}

	analyzeFile = filepath.Join(dir, "missing.go")
	c, _ = bufCmd()
	if err := runAnalyze(c, nil); err == nil || !strings.Contains(err.Error(), "reading file") {
		t.Fatalf("err = %v, want a reading-file error", err)
	}
}

// TestRunAnalyze_DirFlag covers --dir with the detail output, and a missing
// directory.
func TestRunAnalyze_DirFlag(t *testing.T) {
	resetAnalyzeFlags(t)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "pkg", "load.go"), unsafeGo)
	analyzeDir = dir
	analyzeFormat = "detail"

	c, buf := bufCmd()
	if err := runAnalyze(c, nil); err != nil {
		t.Fatalf("runAnalyze: %v", err)
	}
	for _, want := range []string{"AI Code Quality Analysis Report", "ai-unsafe-deserialization", "Suggestions:"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("detail output missing %q:\n%s", want, buf.String())
		}
	}

	analyzeDir = filepath.Join(dir, "missing")
	c, _ = bufCmd()
	if err := runAnalyze(c, nil); err == nil || !strings.Contains(err.Error(), "reading directory") {
		t.Fatalf("err = %v, want a reading-directory error", err)
	}
}

// TestRunAnalyze_GitDiff covers the default input, the diff over the resolved
// base: code changes are analyzed, a docs-only change is a valid empty result
// that says so, and outside a git repository analyze fails loudly.
func TestRunAnalyze_GitDiff(t *testing.T) {
	resetAnalyzeFlags(t)
	clearPipelineEnv(t)
	repo := newRepo(t)
	first := gitRun(t, repo, "rev-parse", "HEAD")
	mustWrite(t, filepath.Join(repo, "svc.go"), unsafeGo)
	commitAll(t, repo, "feat: svc")
	analyzeJSON = true

	c, buf := bufCmd()
	if err := runAnalyze(c, nil); err != nil {
		t.Fatalf("runAnalyze: %v", err)
	}
	if res := decodeAnalysis(t, buf.Bytes()); !hasIssue(res, "ai-unsafe-deserialization", "svc.go") {
		t.Errorf("issues = %+v, want the finding in the added svc.go lines", res.Issues)
	}

	t.Run("docs-only change", func(t *testing.T) {
		mustWrite(t, filepath.Join(repo, "README.md"), "# fixture\n\nmore docs\n")
		commitAll(t, repo, "docs: more")
		c, buf := bufCmd()
		if err := runAnalyze(c, nil); err != nil {
			t.Fatalf("a docs-only change is not a failure: %v", err)
		}
		res := decodeAnalysis(t, buf.Bytes())
		if res.Summary != "No analyzable code in this change" || len(res.Issues) != 0 {
			t.Errorf("result = %+v, want the no-analyzable-code summary", res)
		}
	})

	t.Run("explicit diff base", func(t *testing.T) {
		analyzeDiffBase = first
		c, buf := bufCmd()
		if err := runAnalyze(c, nil); err != nil {
			t.Fatalf("runAnalyze: %v", err)
		}
		if res := decodeAnalysis(t, buf.Bytes()); !hasIssue(res, "ai-unsafe-deserialization", "svc.go") {
			t.Errorf("--diff-base %s should reach back to svc.go; issues = %+v", first, res.Issues)
		}
	})

	t.Run("not a git repository", func(t *testing.T) {
		analyzeDiffBase = ""
		t.Chdir(t.TempDir())
		c, _ := bufCmd()
		err := runAnalyze(c, nil)
		if err == nil || !strings.Contains(err.Error(), "no input provided") {
			t.Fatalf("err = %v, want the no-input error", err)
		}
	})
}

// TestRunAnalyze_SARIFLoadFailureWarns: an unreadable SARIF file is a warning
// on stderr, not a failure; the built-in analysis still reports.
func TestRunAnalyze_SARIFLoadFailureWarns(t *testing.T) {
	resetAnalyzeFlags(t)
	dir := t.TempDir()
	analyzeFile = filepath.Join(dir, "load.go")
	mustWrite(t, analyzeFile, unsafeGo)
	analyzeSARIF = filepath.Join(dir, "missing.sarif")
	analyzeJSON = true

	c, out, stderr := splitCmd()
	if err := runAnalyze(c, nil); err != nil {
		t.Fatalf("runAnalyze: %v", err)
	}
	if !strings.Contains(stderr.String(), "Warning: loading SARIF file") {
		t.Errorf("stderr = %q, want a SARIF warning", stderr.String())
	}
	if res := decodeAnalysis(t, out.Bytes()); !hasIssue(res, "ai-unsafe-deserialization", analyzeFile) {
		t.Errorf("built-in findings were lost: %+v", res.Issues)
	}
}

// TestPrintAnalyzeSummary_CriticalAndTruncation: a critical finding switches
// the icon, the list stops at five, and the first available suggestion is shown.
func TestPrintAnalyzeSummary_CriticalAndTruncation(t *testing.T) {
	var issues []analyzer.Issue
	for i := 0; i < 6; i++ {
		issues = append(issues, analyzer.Issue{Rule: "r", File: "f.go", Line: i + 1, Severity: "low"})
	}
	issues[0].Severity = "critical"
	issues[3].Suggestion = "fix the fourth"
	c, buf := bufCmd()
	printAnalyzeSummary(c, &analyzer.AnalysisResult{Summary: "6 issue(s)", TotalLines: 100, Issues: issues})
	out := buf.String()
	for _, want := range []string{"❌  6 issue(s)", "... and 1 more", "fix the fourth"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "f.go:6") {
		t.Errorf("summary should list only five issues:\n%s", out)
	}
}

// TestPrintAnalyzeDetail_StatusIcons: no findings print a clean report, and a
// non-critical finding gets the warning icon.
func TestPrintAnalyzeDetail_StatusIcons(t *testing.T) {
	c, buf := bufCmd()
	printAnalyzeDetail(c, &analyzer.AnalysisResult{Summary: "No quality issues detected"})
	if out := buf.String(); !strings.HasPrefix(out, "✅") || !strings.Contains(out, "No quality issues detected.") {
		t.Errorf("clean detail output:\n%s", out)
	}

	c, buf = bufCmd()
	printAnalyzeDetail(c, &analyzer.AnalysisResult{Issues: []analyzer.Issue{{Rule: "r", Severity: "medium"}}})
	if out := buf.String(); !strings.HasPrefix(out, "⚠️") {
		t.Errorf("a medium finding should get the warning icon:\n%s", out)
	}
}

// TestDiffHelpers covers the three diff readers on one change: code files with
// their added lines, every changed path, and the new-file numbers of the added
// lines. A ref that does not resolve is an error for all three.
func TestDiffHelpers(t *testing.T) {
	repo := newRepo(t)
	mustWrite(t, filepath.Join(repo, "calc.go"), "package calc\n\nvar A = 1\n\nvar Z = 26\n")
	commitAll(t, repo, "feat: add")
	mustWrite(t, filepath.Join(repo, "calc.go"), "package calc\n\nvar A = 2\nvar B = 3\n\nvar Z = 26\nvar Y = 25\n")
	mustWrite(t, filepath.Join(repo, "README.md"), "# fixture\n\nmore\n")
	commitAll(t, repo, "feat: sub")

	files, err := getGitDiffFiles("HEAD~1")
	if err != nil {
		t.Fatalf("getGitDiffFiles: %v", err)
	}
	wantLines := []string{"var A = 2", "var B = 3", "var Y = 25"}
	if len(files) != 1 || !reflect.DeepEqual(files["calc.go"], wantLines) {
		t.Errorf("getGitDiffFiles = %q, want only calc.go with %q", files, wantLines)
	}

	all, err := getAllChangedFiles("HEAD~1")
	if err != nil {
		t.Fatalf("getAllChangedFiles: %v", err)
	}
	if !reflect.DeepEqual(all, []string{"README.md", "calc.go"}) {
		t.Errorf("getAllChangedFiles = %q, want README.md and calc.go", all)
	}

	nums, err := getDiffAddedLineNumbers("HEAD~1")
	if err != nil {
		t.Fatalf("getDiffAddedLineNumbers: %v", err)
	}
	if want := map[string][]int{"calc.go": {3, 4, 7}}; !reflect.DeepEqual(nums, want) {
		t.Errorf("getDiffAddedLineNumbers = %v, want %v", nums, want)
	}

	for name, fn := range map[string]func(string) error{
		"getGitDiffFiles":         func(b string) error { _, err := getGitDiffFiles(b); return err },
		"getAllChangedFiles":      func(b string) error { _, err := getAllChangedFiles(b); return err },
		"getDiffAddedLineNumbers": func(b string) error { _, err := getDiffAddedLineNumbers(b); return err },
	} {
		if err := fn("no-such-ref"); err == nil {
			t.Errorf("%s(no-such-ref) = nil error, want one", name)
		}
	}
}

// TestDiffHelpers_SkipUnreadableFileDiff: when the per-file diff of one path
// fails (here a file name git parses as pathspec magic), that file is skipped
// and the others are still read.
func TestDiffHelpers_SkipUnreadableFileDiff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file names with ':' are not valid on Windows")
	}
	repo := newRepo(t)
	mustWrite(t, filepath.Join(repo, ":(bad)x.go"), "package p\n")
	mustWrite(t, filepath.Join(repo, "ok.go"), "package q\n")
	commitAll(t, repo, "feat: two files")

	files, err := getGitDiffFiles("HEAD~1")
	if err != nil {
		t.Fatalf("getGitDiffFiles: %v", err)
	}
	if _, ok := files["ok.go"]; !ok || len(files) != 1 {
		t.Errorf("getGitDiffFiles = %q, want only ok.go", files)
	}
	nums, err := getDiffAddedLineNumbers("HEAD~1")
	if err != nil {
		t.Fatalf("getDiffAddedLineNumbers: %v", err)
	}
	if want := map[string][]int{"ok.go": {1}}; !reflect.DeepEqual(nums, want) {
		t.Errorf("getDiffAddedLineNumbers = %v, want %v", nums, want)
	}
}

// TestAddedLineNumbers walks a two-hunk diff: context and added lines advance
// the new-file counter, removed lines and the no-newline marker do not, and the
// file header before the first hunk is ignored.
func TestAddedLineNumbers(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n" +
		"@@ -1,3 +1,4 @@\n ctx1\n-old\n+new1\n+new2\n ctx2\n\\ No newline at end of file\n" +
		"@@ -10,2 +11,2 @@\n ctx\n+tail\n"
	if got, want := addedLineNumbers([]byte(diff)), []int{2, 3, 12}; !reflect.DeepEqual(got, want) {
		t.Errorf("addedLineNumbers = %v, want %v", got, want)
	}
	if got := addedLineNumbers([]byte("+++ b/x.go\n+not in a hunk\n")); got != nil {
		t.Errorf("lines before the first hunk = %v, want none", got)
	}
}

// TestParseHunkNewStart reads the new-file start from a hunk header and
// returns 0 for a header it cannot parse.
func TestParseHunkNewStart(t *testing.T) {
	cases := map[string]int{
		"@@ -1,2 +5,3 @@":        5,
		"@@ -0,0 +1 @@ func x()": 1,
		"@@ no plus sign @@":     0,
		"@@ -1 +x @@":            0,
	}
	for hunk, want := range cases {
		if got := parseHunkNewStart(hunk); got != want {
			t.Errorf("parseHunkNewStart(%q) = %d, want %d", hunk, got, want)
		}
	}
}

// TestWalkDir_UnreadableSubdir: an error reading a nested directory stops the
// walk and is returned, rather than producing a silently partial result.
func TestWalkDir_UnreadableSubdir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "locked")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(sub, 0o755) })

	if err := walkDir(dir, func(string, []byte) {}); err == nil {
		t.Error("walkDir over an unreadable subdirectory returned nil, want an error")
	}
}
