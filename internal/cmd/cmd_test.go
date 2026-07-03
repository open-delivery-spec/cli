package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/open-delivery-spec/cli/internal/rules"
	"github.com/open-delivery-spec/cli/internal/scorer"
	"github.com/spf13/cobra"
)

// bufCmd returns a bare cobra.Command whose stdout/stderr are captured in buf.
func bufCmd() (*cobra.Command, *bytes.Buffer) {
	c := &cobra.Command{}
	buf := &bytes.Buffer{}
	c.SetOut(buf)
	c.SetErr(buf)
	return c, buf
}

// ─── pure helpers ────────────────────────────────────────────────

func TestSeverityIcon(t *testing.T) {
	cases := map[string]string{
		"critical": "🔴",
		"high":     "🟠",
		"medium":   "🟡",
		"low":      "🔵",
		"info":     "⚪",
		"unknown":  "⚪",
	}
	for sev, want := range cases {
		if got := severityIcon(sev); got != want {
			t.Errorf("severityIcon(%q) = %q, want %q", sev, got, want)
		}
	}
}

func TestIsCodeFileExt(t *testing.T) {
	code := []string{"main.go", "app.py", "index.ts", "Foo.java", "lib.rs", "a.tsx", "x.CPP"}
	for _, f := range code {
		if !isCodeFileExt(f) {
			t.Errorf("isCodeFileExt(%q) = false, want true", f)
		}
	}
	notCode := []string{"README.md", "go.mod", "data.json", "config.yaml", "image.png", "notes.txt"}
	for _, f := range notCode {
		if isCodeFileExt(f) {
			t.Errorf("isCodeFileExt(%q) = true, want false", f)
		}
	}
}

func TestExtractAdded(t *testing.T) {
	diff := []byte(`diff --git a/x.go b/x.go
index 111..222 100644
--- a/x.go
+++ b/x.go
@@ -1,2 +1,3 @@
 unchanged line
+added line one
+added line two
-removed line
`)
	got := extractAdded(diff)
	want := []string{"added line one", "added line two"}
	if len(got) != len(want) {
		t.Fatalf("extractAdded returned %d lines, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractAdded_IgnoresFileHeader(t *testing.T) {
	// The "+++ b/file" header starts with "++" and must not be treated as added content.
	diff := []byte("+++ b/x.go\n+real addition\n")
	got := extractAdded(diff)
	if len(got) != 1 || got[0] != "real addition" {
		t.Errorf("extractAdded = %v, want [real addition]", got)
	}
}

func TestReadEnvStr(t *testing.T) {
	t.Setenv("ODS_TEST_VAR", "hello")
	if got := readEnvStr("ODS_TEST_VAR"); got != "hello" {
		t.Errorf("readEnvStr = %q, want hello", got)
	}
	if got := readEnvStr("ODS_DEFINITELY_UNSET_VAR"); got != "" {
		t.Errorf("readEnvStr(unset) = %q, want empty", got)
	}
}

// ─── filesystem helpers ──────────────────────────────────────────

func TestReadDir(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package a\nfunc A() {}\n")
	mustWrite(t, filepath.Join(dir, "sub", "b.py"), "def b():\n    pass\n")
	mustWrite(t, filepath.Join(dir, "README.md"), "# not code\n")
	mustWrite(t, filepath.Join(dir, ".hidden", "c.go"), "package c\n") // hidden dir skipped

	files, err := readDir(dir)
	if err != nil {
		t.Fatalf("readDir: %v", err)
	}
	if _, ok := files[filepath.Join(dir, "a.go")]; !ok {
		t.Error("expected a.go in results")
	}
	if _, ok := files[filepath.Join(dir, "sub", "b.py")]; !ok {
		t.Error("expected sub/b.py in results")
	}
	if _, ok := files[filepath.Join(dir, "README.md")]; ok {
		t.Error("README.md should be excluded (not a code file)")
	}
	if _, ok := files[filepath.Join(dir, ".hidden", "c.go")]; ok {
		t.Error("files under hidden directories should be skipped")
	}
}

func TestCountTestDirLines(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "foo_test.go"), "package foo\n\nfunc TestFoo(t *testing.T) {}\n") // 3 lines
	mustWrite(t, filepath.Join(dir, "test_bar.py"), "def test_bar():\n    assert True\n")             // 2 lines
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n")                                     // not a test file

	n := countTestDirLines(dir)
	if n < 4 {
		t.Errorf("countTestDirLines = %d, want >= 4 (test files only)", n)
	}
}

// ─── output formatters ───────────────────────────────────────────

func TestPrintAnalyzeSummary_NoIssues(t *testing.T) {
	c, buf := bufCmd()
	printAnalyzeSummary(c, &analyzer.AnalysisResult{Summary: "No quality issues detected", Issues: nil})
	if !strings.Contains(buf.String(), "✅") {
		t.Errorf("expected success icon, got: %s", buf.String())
	}
}

func TestPrintAnalyzeSummary_WithIssues(t *testing.T) {
	c, buf := bufCmd()
	res := &analyzer.AnalysisResult{
		Summary:    "1 issue found",
		TotalLines: 100,
		Issues: []analyzer.Issue{
			{Rule: "ai-over-commenting", File: "x.go", Line: 1, Severity: "high", Message: "too many comments", Suggestion: "remove them"},
		},
	}
	printAnalyzeSummary(c, res)
	out := buf.String()
	if !strings.Contains(out, "ai-over-commenting") {
		t.Errorf("expected rule name in output, got: %s", out)
	}
	if !strings.Contains(out, "💡") {
		t.Errorf("expected suggestion hint, got: %s", out)
	}
}

func TestPrintAnalyzeDetail(t *testing.T) {
	c, buf := bufCmd()
	res := &analyzer.AnalysisResult{
		Summary:    "1 issue",
		TotalLines: 50,
		Issues: []analyzer.Issue{
			{Rule: "ai-unsafe-deserialization", File: "y.go", Line: 10, Severity: "critical", Message: "unsafe", Suggestion: "use a struct"},
		},
	}
	printAnalyzeDetail(c, res)
	out := buf.String()
	for _, want := range []string{"AI Code Quality Analysis Report", "ai-unsafe-deserialization", "Suggestions:"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q\n%s", want, out)
		}
	}
}

func TestPrintScoreSummaryAndDetail(t *testing.T) {
	res := &scorer.ScoreResult{
		TechnicalDebtDelta: 2.5,
		Verdict:            "neutral",
		Recommendation:     "Moderate risk",
		Breakdown: scorer.ScoreBreakdown{
			AICodeRatio: 0.5, DefectDensity: 1.0, CriticalIssues: 0,
			TestCoverage: 0.8, DuplicationRate: 0.1,
		},
	}

	c, buf := bufCmd()
	printScoreSummary(c, res)
	if !strings.Contains(buf.String(), "neutral") {
		t.Errorf("summary missing verdict: %s", buf.String())
	}

	c2, buf2 := bufCmd()
	printScoreDetail(c2, res)
	for _, want := range []string{"Technical Debt Score Report", "AI Code Ratio", "Test Coverage"} {
		if !strings.Contains(buf2.String(), want) {
			t.Errorf("detail missing %q\n%s", want, buf2.String())
		}
	}
}

func TestPrintCheckResult(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		c, buf := bufCmd()
		printCheckResult(c, &policy.EvalResult{Allowed: true}, "", true)
		out := buf.String()
		if !strings.Contains(out, "passed") {
			t.Errorf("expected passed message, got: %s", out)
		}
		if !strings.Contains(out, "default (built-in)") {
			t.Errorf("expected default policy label, got: %s", out)
		}
	})
	t.Run("denied with warnings", func(t *testing.T) {
		c, buf := bufCmd()
		res := &policy.EvalResult{
			Allowed:  false,
			Denials:  []string{"critical issue found"},
			Warnings: []string{"low coverage"},
		}
		printCheckResult(c, res, ".ods/policy.rego", false)
		out := buf.String()
		for _, want := range []string{"failed", "critical issue found", "low coverage", ".ods/policy.rego"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q\n%s", want, out)
			}
		}
	})
}

func TestPrintDetect(t *testing.T) {
	res := &detector.DetectionResult{
		AIGenerated: true,
		Confidence:  0.9,
		Summary:     "AI code detected",
		Sources:     []string{"commit-trailer", "branch-name"},
		Evidence: []detector.Evidence{
			{Source: "commit-trailer", Value: "Claude", Confidence: 0.95},
		},
	}

	c, buf := bufCmd()
	printSummary(c, res)
	if !strings.Contains(buf.String(), "AI code detected") {
		t.Errorf("summary missing detection text: %s", buf.String())
	}

	c2, buf2 := bufCmd()
	printDetailed(c2, res)
	for _, want := range []string{"AI Code Detection Report", "Confidence", "Risk Level: High"} {
		if !strings.Contains(buf2.String(), want) {
			t.Errorf("detail missing %q\n%s", want, buf2.String())
		}
	}
}

// ─── default policy evaluation ───────────────────────────────────

func TestEvaluateDefaultPolicy(t *testing.T) {
	t.Run("clean input allowed", func(t *testing.T) {
		res, err := evaluateDefaultPolicy(&policy.EvalInput{
			AIGenerated:  false,
			TestCoverage: 0.9,
		})
		if err != nil {
			t.Fatalf("evaluateDefaultPolicy: %v", err)
		}
		if !res.Allowed {
			t.Errorf("clean input: allowed = false, want true (denials: %v)", res.Denials)
		}
	})

	t.Run("critical issue denied", func(t *testing.T) {
		res, err := evaluateDefaultPolicy(&policy.EvalInput{
			Issues: []policy.EvalIssue{
				{Rule: "ai-unsafe-deserialization", File: "x.go", Line: 1, Severity: "critical", Message: "unsafe"},
			},
		})
		if err != nil {
			t.Fatalf("evaluateDefaultPolicy: %v", err)
		}
		if res.Allowed {
			t.Errorf("critical issue: allowed = true, want false")
		}
	})
}

// ─── rules command ───────────────────────────────────────────────

func TestRunRules_Table(t *testing.T) {
	t.Cleanup(func() { rulesJSON = false })
	rulesJSON = false
	c, buf := bufCmd()
	if err := runRules(c, nil); err != nil {
		t.Fatalf("runRules: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ODS Analysis Rules") {
		t.Errorf("table output missing header: %s", out)
	}
	// Every registered rule ID should appear in the table.
	for _, id := range rules.IDs() {
		if !strings.Contains(out, id) {
			t.Errorf("table output missing rule %q", id)
		}
	}
}

func TestRunRules_JSON(t *testing.T) {
	t.Cleanup(func() { rulesJSON = false })
	rulesJSON = true
	c, buf := bufCmd()
	if err := runRules(c, nil); err != nil {
		t.Fatalf("runRules: %v", err)
	}
	var got []rules.Rule
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("rules --json did not produce valid JSON: %v\n%s", err, buf.String())
	}
	if len(got) != len(rules.All()) {
		t.Errorf("json rules count = %d, want %d", len(got), len(rules.All()))
	}
}

// TestRunAnalyze_SARIFWithoutLocalCode guards the multi-language path: when
// --sarif is given but the diff has no analyzable code (e.g. a docs-only PR, or
// a non-Go repo), analyze must still succeed and emit the external findings
// rather than erroring with "no input provided".
func TestRunAnalyze_SARIFWithoutLocalCode(t *testing.T) {
	t.Cleanup(func() {
		analyzeSARIF, analyzeJSON, analyzeFile, analyzeDir = "", false, "", ""
	})
	dir := t.TempDir()
	t.Chdir(dir) // not a git repo → default diff path finds no code
	sarifPath := filepath.Join(dir, "ext.sarif")
	mustWrite(t, sarifPath, `{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "semgrep", "rules": [
      {"id": "ext.rule.injection", "properties": {"severity": "ERROR"}}
    ]}},
    "results": [{
      "ruleId": "ext.rule.injection",
      "level": "error",
      "message": {"text": "command injection"},
      "locations": [{"physicalLocation": {
        "artifactLocation": {"uri": "app/run.py"},
        "region": {"startLine": 7}
      }}]
    }]
  }]
}`)
	analyzeSARIF = sarifPath
	analyzeJSON = true

	c, buf := bufCmd()
	if err := runAnalyze(c, nil); err != nil {
		t.Fatalf("analyze --sarif with no local code should succeed, got: %v", err)
	}
	var res analyzer.AnalysisResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	found := false
	for _, iss := range res.Issues {
		if iss.Rule == "ext.rule.injection" {
			found = true
			if iss.Severity != "high" {
				t.Errorf("severity = %s, want high (error→high)", iss.Severity)
			}
		}
	}
	if !found {
		t.Errorf("SARIF finding was not merged; issues=%+v", res.Issues)
	}
}

// TestRunCheck_SARIFReachesPolicy proves external SARIF findings reach the
// policy gate: a high-severity SARIF finding plus a policy that denies on high
// severity must block, even when there is no local code to analyze.
func TestRunCheck_SARIFReachesPolicy(t *testing.T) {
	t.Cleanup(func() { checkSARIF, checkPolicyFile, checkJSON = "", "", false })
	dir := t.TempDir()
	t.Chdir(dir) // non-git → no built-in issues; the SARIF must carry the finding
	sarifPath := filepath.Join(dir, "ext.sarif")
	mustWrite(t, sarifPath, `{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "semgrep"}},
    "results": [{
      "ruleId": "ext.rule.injection",
      "level": "error",
      "message": {"text": "command injection"},
      "locations": [{"physicalLocation": {
        "artifactLocation": {"uri": "app/run.py"}, "region": {"startLine": 7}
      }}]
    }]
  }]
}`)
	policyPath := filepath.Join(dir, "policy.rego")
	mustWrite(t, policyPath, `package ods.policy
default allow := true
deny[msg] {
    issue := input.issues[_]
    issue.severity == "high"
    msg := sprintf("%s at %s:%d", [issue.rule, issue.file, issue.line])
}`)
	checkSARIF = sarifPath
	checkPolicyFile = policyPath
	checkJSON = true

	c, buf := bufCmd()
	if err := runCheck(c, nil); err == nil {
		t.Fatalf("expected a policy denial, got nil; out=%s", buf.String())
	}
	var res policy.EvalResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("check output not valid JSON: %v\n%s", err, buf.String())
	}
	if res.Allowed {
		t.Errorf("expected allowed=false from a high SARIF finding, got true")
	}
	found := false
	for _, d := range res.Denials {
		if strings.Contains(d, "ext.rule.injection") {
			found = true
		}
	}
	if !found {
		t.Errorf("denials %v should reference the SARIF rule id", res.Denials)
	}
}

// TestRunScore_SARIFCountsAsCritical proves SARIF high/critical findings feed
// the debt score (CriticalIssues), not just the analyze report.
func TestRunScore_SARIFCountsAsCritical(t *testing.T) {
	t.Cleanup(func() { scoreSARIF, scoreJSON = "", false })
	dir := t.TempDir()
	t.Chdir(dir)
	sarifPath := filepath.Join(dir, "ext.sarif")
	mustWrite(t, sarifPath, `{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "semgrep"}},
    "results": [{
      "ruleId": "ext.rule.injection",
      "level": "error",
      "message": {"text": "command injection"},
      "locations": [{"physicalLocation": {
        "artifactLocation": {"uri": "app/run.py"}, "region": {"startLine": 7}
      }}]
    }]
  }]
}`)
	scoreSARIF = sarifPath
	scoreJSON = true

	c, buf := bufCmd()
	if err := runScore(c, nil); err != nil {
		t.Fatalf("runScore: %v", err)
	}
	var res scorer.ScoreResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("score output not valid JSON: %v\n%s", err, buf.String())
	}
	if res.Breakdown.CriticalIssues < 1 {
		t.Errorf("critical_issues = %d, want >= 1 from a high SARIF finding", res.Breakdown.CriticalIssues)
	}
	if res.TechnicalDebtDelta <= 0 {
		t.Errorf("delta = %f, want > 0 from a high SARIF finding", res.TechnicalDebtDelta)
	}
}

func TestResolveDiffBase(t *testing.T) {
	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv("ODS_DIFF_BASE", "origin/main")
		if got := resolveDiffBase("HEAD~3"); got != "HEAD~3" {
			t.Errorf("got %q, want HEAD~3", got)
		}
	})
	t.Run("env used when flag empty", func(t *testing.T) {
		t.Setenv("ODS_DIFF_BASE", "origin/main")
		if got := resolveDiffBase(""); got != "origin/main" {
			t.Errorf("got %q, want origin/main", got)
		}
	})
	t.Run("falls back to HEAD~1", func(t *testing.T) {
		t.Setenv("ODS_DIFF_BASE", "")
		if got := resolveDiffBase(""); got != "HEAD~1" {
			t.Errorf("got %q, want HEAD~1", got)
		}
	})
}

// ─── helpers ─────────────────────────────────────────────────────

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScaffoldPolicyReviewTier guards the policy that `ods init` writes into
// user repos: it must stay valid Rego and route review tiers as its comments
// promise.
func TestScaffoldPolicyReviewTier(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "scaffold-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(defaultPolicy); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cases := []struct {
		name string
		in   *policy.EvalInput
		want string
	}{
		{"clean low-delta change routes auto", &policy.EvalInput{TechnicalDebtDelta: 0.5}, "auto"},
		{"AI change with high issue routes elevated", &policy.EvalInput{
			AIGenerated: true,
			Issues:      []policy.EvalIssue{{Rule: "x", Severity: "high", File: "a.go"}},
		}, "elevated"},
		{"mid-delta change routes standard", &policy.EvalInput{TechnicalDebtDelta: 2.0}, "standard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := policy.Evaluate(f.Name(), tc.in)
			if err != nil {
				t.Fatalf("scaffold policy failed to evaluate: %v", err)
			}
			if result.ReviewTier != tc.want {
				t.Errorf("review_tier = %q, want %q", result.ReviewTier, tc.want)
			}
		})
	}
}
