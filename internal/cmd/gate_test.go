package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-delivery-spec/cli/internal/policy"
)

// svcGo is five added lines; line 3 opens the function, line 4 is its body.
const svcGo = "package svc\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"

// gateRepo builds a repository whose last commit is AI-attributed and adds
// svc.go plus a docs page, makes it the working directory, and returns its
// path and head SHA.
func gateRepo(t *testing.T) (string, string) {
	t.Helper()
	clearPipelineEnv(t)
	repo := newRepo(t)
	mustWrite(t, filepath.Join(repo, "svc.go"), svcGo)
	mustWrite(t, filepath.Join(repo, "docs", "guide.md"), "# guide\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "feat: add svc\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
	return repo, gitRun(t, repo, "rev-parse", "HEAD")
}

// resetGateFlags restores the check and attest commands' flag state and the
// shared --pr-file flag the pipeline reads.
func resetGateFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		checkInputFile, checkPolicyFile, checkSARIF, checkMutation, checkDiffBase = "", "", "", "", ""
		checkJSON = false
		checkAIReviews = nil
		attestPolicyFile, attestSARIF, attestDiffBase, attestMutation = "", "", "", ""
		attestAIReviews = nil
		attestOut = "evidence.cdx.json"
		detectPRFile = ""
	})
}

// checkOutput is the JSON `ods check` prints in the pipeline mode.
type checkOutput struct {
	policy.EvalResult
	MergeConfidence *policy.EvalMergeConfidence `json:"merge_confidence"`
	PatchCoverage   float64                     `json:"patch_coverage"`
	MutationScore   float64                     `json:"mutation_score"`
	EvidenceTier    string                      `json:"evidence_tier"`
}

// echoPolicy allows everything and echoes selected input fields as warnings,
// so a test can see exactly what reached the policy.
const echoPolicy = `package ods.policy
default allow := true
warn[msg] {
    r := input.ai_reviews[_]
    msg := sprintf("review %s %s %d", [r.tool, r.verdict, count(r.findings)])
}
warn[msg] {
    f := input.ai_files[_]
    msg := sprintf("ai file %s %d/%d", [f.path, f.ai_lines, f.total_lines])
}
warn[msg] {
    msg := sprintf("changed %d", [count(input.changed_files)])
}
`

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestRunCheck_GateInputs runs the gate over a real change and checks every
// signal the input assembly adds: AI-attributed files, changed paths, patch
// coverage from a per-line report, the diff-scoped mutation score, and AI
// review verdicts, of which stale and malformed ones are skipped with a warning.
func TestRunCheck_GateInputs(t *testing.T) {
	resetGateFlags(t)
	repo, head := gateRepo(t)
	dir := t.TempDir()

	// Per-line coverage: line 3 is hit, line 4 is not.
	mustWrite(t, filepath.Join(repo, "coverage.out"),
		"mode: set\ngithub.com/acme/svc/svc.go:3.25,3.26 1 1\ngithub.com/acme/svc/svc.go:4.2,4.14 1 0\n")
	checkMutation = filepath.Join(dir, "gremlins.json")
	mustWrite(t, checkMutation, `{"files":[{"file_name":"svc.go","mutations":[
	  {"line":4,"status":"KILLED"},{"line":4,"status":"LIVED"},{"line":99,"status":"KILLED"}]}]}`)

	good := filepath.Join(dir, "good.json")
	mustWrite(t, good, `{"schema":"ods.dev/review-verdict/v1","reviewer":{"tool":"claude-code","model":"m"},
	  "head_sha":"`+head+`","verdict":"request_changes",
	  "findings":[{"file":"svc.go","line":4,"severity":"high","category":"correctness","message":"overflow"}]}`)
	stale := filepath.Join(dir, "stale.json")
	mustWrite(t, stale, `{"schema":"ods.dev/review-verdict/v1","reviewer":{"tool":"other"},"head_sha":"deadbeef","verdict":"approve"}`)
	bad := filepath.Join(dir, "bad.json")
	mustWrite(t, bad, `{"schema":"ods.dev/review-verdict/v1","reviewer":{"tool":"x"},"verdict":"maybe"}`)
	checkAIReviews = []string{good, stale, bad}

	checkPolicyFile = filepath.Join(dir, "policy.rego")
	mustWrite(t, checkPolicyFile, echoPolicy)
	checkJSON = true

	c, out, stderr := splitCmd()
	if err := runCheck(c, nil); err != nil {
		t.Fatalf("runCheck: %v\nstderr: %s", err, stderr)
	}
	var res checkOutput
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("check output not valid JSON: %v\n%s", err, out)
	}
	if res.PatchCoverage != 0.5 {
		t.Errorf("patch_coverage = %v, want 0.5 (line 3 hit, line 4 not)", res.PatchCoverage)
	}
	if res.MutationScore != 0.5 {
		t.Errorf("mutation_score = %v, want 0.5 (one killed, one lived on line 4)", res.MutationScore)
	}
	if res.EvidenceTier != "attested" {
		t.Errorf("evidence_tier = %q, want attested from the trailer", res.EvidenceTier)
	}
	if mc := res.MergeConfidence; mc == nil || mc.FilesChanged != 2 || mc.SourceFilesChanged != 1 || !mc.AddedSourceWithoutTests {
		t.Errorf("merge_confidence = %+v, want 2 files, 1 source file, no tests", mc)
	}
	for _, want := range []string{"review claude-code request_changes 1", "ai file svc.go 5/5", "changed 2"} {
		if !contains(res.Warnings, want) {
			t.Errorf("warnings %q: missing %q", res.Warnings, want)
		}
	}
	if contains(res.Warnings, "review other approve 0") {
		t.Error("a verdict stamped for another commit must not reach the policy")
	}
	for _, want := range []string{"verdict is for commit deadbeef", "Warning: skipping AI review"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

// TestRunCheck_TextOutputAndPolicyDiscovery covers the human-readable result
// with a discovered .ods/policy.rego, the review tier line, and the fallback to
// the built-in policy when no policy file exists.
func TestRunCheck_TextOutputAndPolicyDiscovery(t *testing.T) {
	resetGateFlags(t)
	repo, _ := gateRepo(t)
	mustWrite(t, filepath.Join(repo, ".ods", "policy.rego"), "package ods.policy\ndefault allow := true\nreview_tier := \"elevated\"\n")

	c, buf := bufCmd()
	if err := runCheck(c, nil); err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	for _, want := range []string{"Policy check passed", "Policy: ./.ods/policy.rego", "Review tier: elevated"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output missing %q:\n%s", want, buf)
		}
	}

	if err := os.RemoveAll(filepath.Join(repo, ".ods")); err != nil {
		t.Fatal(err)
	}
	c, buf = bufCmd()
	if err := runCheck(c, nil); err != nil {
		t.Fatalf("runCheck with the default policy: %v", err)
	}
	if !strings.Contains(buf.String(), "Policy: default (built-in)") {
		t.Errorf("output should name the built-in policy:\n%s", buf)
	}
}

// TestRunCheck_Failures covers the ways the gate stops: a policy denial, a
// policy that does not compile, and a mutation report that cannot be read.
func TestRunCheck_Failures(t *testing.T) {
	resetGateFlags(t)
	gateRepo(t)
	dir := t.TempDir()

	t.Run("denied", func(t *testing.T) {
		checkPolicyFile = filepath.Join(dir, "deny.rego")
		mustWrite(t, checkPolicyFile, "package ods.policy\ndeny[msg] { msg := \"no\" }\n")
		c, buf := bufCmd()
		err := runCheck(c, nil)
		if err == nil || !strings.Contains(err.Error(), "policy denied: 1 denial(s)") {
			t.Fatalf("err = %v, want a denial", err)
		}
		if !c.SilenceUsage || !strings.Contains(buf.String(), "Policy check failed") {
			t.Errorf("a denial should print the result without usage:\n%s", buf)
		}
	})

	t.Run("policy does not compile", func(t *testing.T) {
		checkPolicyFile = filepath.Join(dir, "broken.rego")
		mustWrite(t, checkPolicyFile, "package ods.policy\ndeny[msg] {\n")
		c, _ := bufCmd()
		if err := runCheck(c, nil); err == nil || !strings.Contains(err.Error(), "policy evaluation failed") {
			t.Fatalf("err = %v, want a policy evaluation error", err)
		}
	})

	t.Run("unreadable mutation report", func(t *testing.T) {
		checkPolicyFile = ""
		checkMutation = filepath.Join(dir, "missing.json")
		c, _ := bufCmd()
		if err := runCheck(c, nil); err == nil {
			t.Fatal("runCheck with a missing mutation report returned nil")
		}
	})
}

// TestRunCheck_PreparedInputPolicies covers --input against the built-in
// policy (which denies a critical issue), a discovered policy file, and a
// policy that fails to compile.
func TestRunCheck_PreparedInputPolicies(t *testing.T) {
	resetGateFlags(t)
	dir := t.TempDir()
	t.Chdir(dir)
	checkInputFile = filepath.Join(dir, "input.json")
	mustWrite(t, checkInputFile, `{"issues":[{"rule":"r","file":"a.go","line":3,"severity":"critical","message":"m"}],
	  "ai_files":[],"changed_files":["a.go"]}`)

	c, buf := bufCmd()
	err := runCheck(c, nil)
	if err == nil || !strings.Contains(err.Error(), "policy denied") {
		t.Fatalf("err = %v, want the built-in policy to deny a critical issue", err)
	}
	for _, want := range []string{"Policy check failed", "Policy: default (built-in)", "CRITICAL: r at a.go:3"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output missing %q:\n%s", want, buf)
		}
	}

	mustWrite(t, filepath.Join(dir, ".ods", "policy.rego"), "package ods.policy\ndefault allow := true\n")
	c, buf = bufCmd()
	if err := runCheck(c, nil); err != nil {
		t.Fatalf("the discovered policy allows everything: %v", err)
	}
	if !strings.Contains(buf.String(), "Policy: ./.ods/policy.rego") {
		t.Errorf("output should name the discovered policy:\n%s", buf)
	}

	checkPolicyFile = filepath.Join(dir, "broken.rego")
	mustWrite(t, checkPolicyFile, "not rego at all {")
	c, _ = bufCmd()
	if err := runCheck(c, nil); err == nil || !strings.Contains(err.Error(), "policy evaluation failed") {
		t.Fatalf("err = %v, want a policy evaluation error", err)
	}
}

// TestEvaluateDefaultPolicy_NoTempDir: when the built-in policy cannot be
// written to a temporary file, the error is returned instead of a verdict.
func TestEvaluateDefaultPolicy_NoTempDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("TMPDIR", missing)
	if _, err := evaluateDefaultPolicy(&policy.EvalInput{}); err == nil {
		t.Fatal("evaluateDefaultPolicy returned nil error without a usable temp dir")
	}
}

// TestGitHeadSHA: ODS_HEAD_SHA wins, otherwise HEAD of the repository, and ""
// outside one.
func TestGitHeadSHA(t *testing.T) {
	clearPipelineEnv(t)
	repo := newRepo(t)
	head := gitRun(t, repo, "rev-parse", "HEAD")
	if got := gitHeadSHA(); got != head {
		t.Errorf("gitHeadSHA = %q, want HEAD %q", got, head)
	}
	if got := headSHAForAttest(); got != head {
		t.Errorf("headSHAForAttest = %q, want HEAD %q", got, head)
	}
	t.Setenv("ODS_HEAD_SHA", "cafe1234")
	if got, got2 := gitHeadSHA(), headSHAForAttest(); got != "cafe1234" || got2 != "cafe1234" {
		t.Errorf("gitHeadSHA, headSHAForAttest = %q, %q; want the ODS_HEAD_SHA override", got, got2)
	}
	t.Setenv("ODS_HEAD_SHA", "")
	t.Chdir(t.TempDir())
	if got := gitHeadSHA(); got != "" {
		t.Errorf("gitHeadSHA outside a repository = %q, want empty", got)
	}
}

// TestRunURLAndPRNumber: the run URL needs all three GitHub variables, and the
// PR number comes only from a refs/pull/<n>/... ref.
func TestRunURLAndPRNumber(t *testing.T) {
	clearPipelineEnv(t)
	if got := runURLFromEnv(); got != "" {
		t.Errorf("runURLFromEnv without the GitHub environment = %q, want empty", got)
	}
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "acme/widgets")
	t.Setenv("GITHUB_RUN_ID", "7")
	if got, want := runURLFromEnv(), "https://github.com/acme/widgets/actions/runs/7"; got != want {
		t.Errorf("runURLFromEnv = %q, want %q", got, want)
	}
	for ref, want := range map[string]string{
		"refs/pull/42/merge": "42",
		"refs/heads/main":    "",
		"":                   "",
	} {
		if got := prNumberFromRef(ref); got != want {
			t.Errorf("prNumberFromRef(%q) = %q, want %q", ref, got, want)
		}
	}
}

// TestRunAttest covers the evidence document: written to a file by default or
// to stdout with --out -, carrying the change locators from the GitHub
// environment, and the failures that stop it.
func TestRunAttest(t *testing.T) {
	resetGateFlags(t)
	_, head := gateRepo(t)
	dir := t.TempDir()
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "acme/widgets")
	t.Setenv("GITHUB_RUN_ID", "7")
	t.Setenv("GITHUB_REF", "refs/pull/42/merge")
	t.Setenv("ODS_PIPELINE_INTEGRITY", "ok")

	attestOut = filepath.Join(dir, "evidence.cdx.json")
	c, buf := bufCmd()
	if err := runAttest(c, nil); err != nil {
		t.Fatalf("runAttest: %v", err)
	}
	if !strings.Contains(buf.String(), "written to "+attestOut) {
		t.Errorf("output = %q, want the written path", buf)
	}
	data, err := os.ReadFile(attestOut)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("evidence document is not JSON: %v", err)
	}
	if doc["bomFormat"] != "CycloneDX" {
		t.Errorf("bomFormat = %v, want CycloneDX", doc["bomFormat"])
	}
	for _, want := range []string{"acme/widgets PR #42", "https://github.com/acme/widgets/actions/runs/7", head} {
		if !strings.Contains(string(data), want) {
			t.Errorf("evidence document missing %q", want)
		}
	}

	t.Run("stdout with an explicit policy", func(t *testing.T) {
		attestOut = "-"
		attestPolicyFile = filepath.Join(dir, "allow.rego")
		mustWrite(t, attestPolicyFile, "package ods.policy\ndefault allow := true\n")
		c, buf := bufCmd()
		if err := runAttest(c, nil); err != nil {
			t.Fatalf("runAttest: %v", err)
		}
		if !json.Valid(buf.Bytes()) {
			t.Errorf("--out - should print the JSON document:\n%s", buf)
		}
	})

	t.Run("policy evaluation fails", func(t *testing.T) {
		attestPolicyFile = filepath.Join(dir, "broken.rego")
		mustWrite(t, attestPolicyFile, "package ods.policy\ndeny[msg] {\n")
		c, _ := bufCmd()
		if err := runAttest(c, nil); err == nil || !strings.Contains(err.Error(), "policy evaluation failed") {
			t.Fatalf("err = %v, want a policy evaluation error", err)
		}
	})

	t.Run("unreadable mutation report", func(t *testing.T) {
		attestPolicyFile = ""
		attestMutation = filepath.Join(dir, "missing.json")
		c, _ := bufCmd()
		if err := runAttest(c, nil); err == nil {
			t.Fatal("runAttest with a missing mutation report returned nil")
		}
	})

	t.Run("unwritable output", func(t *testing.T) {
		attestMutation = ""
		attestOut = filepath.Join(dir, "no-such-dir", "evidence.cdx.json")
		c, _ := bufCmd()
		if err := runAttest(c, nil); err == nil || !strings.Contains(err.Error(), "writing") {
			t.Fatalf("err = %v, want a write error", err)
		}
	})
}

// TestRunPipeline_Warnings: an unreadable --pr-file or SARIF file degrades to
// a warning, and an explicit coverage report feeds the score.
func TestRunPipeline_Warnings(t *testing.T) {
	resetGateFlags(t)
	gateRepo(t)
	dir := t.TempDir()
	detectPRFile = filepath.Join(dir, "missing-pr.md")
	cov := filepath.Join(dir, "coverage.out")
	mustWrite(t, cov, "mode: set\ngithub.com/acme/svc/svc.go:3.25,5.2 2 1\n")

	c, _, stderr := splitCmd()
	p := runPipeline(c, "HEAD~1", filepath.Join(dir, "missing.sarif"), cov)
	for _, want := range []string{"Warning: reading PR file", "Warning: loading SARIF file"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
	if p.TotalLines != 5 {
		t.Errorf("total lines = %d, want the 5 added lines of svc.go", p.TotalLines)
	}
	if p.Score.Breakdown.TestCoverage != 1 || p.Score.Breakdown.TestCoverageSource != "go" {
		t.Errorf("coverage = %v (%s), want 1 from the Go report", p.Score.Breakdown.TestCoverage, p.Score.Breakdown.TestCoverageSource)
	}
}
