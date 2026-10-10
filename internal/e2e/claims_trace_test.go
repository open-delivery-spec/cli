package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// runODSWithEnv runs the ods binary with extra environment on top of the
// hermetic one, for inputs only the environment carries (ODS_PR_BODY).
func runODSWithEnv(t *testing.T, dir string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(odsBin, args...)
	cmd.Dir = dir
	cmd.Env = append(hermeticEnv(), env...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running ods %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
		}
		exit = ee.ExitCode()
	}
	return stdout.String(), exit
}

// TestPipeline_ClaimsAndTestWeakening runs the gate on an AI-attributed change
// that skips a test, strips its assertions, deletes another test file and
// silences a lint check, under a description that claims tests and docs. The
// facts must reach the policy input, the default policy must warn and route
// to elevated review, and nothing may deny.
func TestPipeline_ClaimsAndTestWeakening(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "calc/calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	writeFile(t, dir, "calc/calc_test.go", `package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Errorf("Add(1, 2) != 3")
	}
	if Add(0, 0) != 0 {
		t.Fatalf("Add(0, 0) != 0")
	}
}
`)
	writeFile(t, dir, "calc/legacy_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestLegacy(t *testing.T) {}\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: add calc")

	writeFile(t, dir, "calc/calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b + 0 } //nolint:staticcheck\n")
	writeFile(t, dir, "calc/calc_test.go", `package calc

import "testing"

func TestAdd(t *testing.T) {
	t.Skip("flaky")
	_ = Add(1, 2)
}
`)
	git(t, dir, "rm", "-q", "calc/legacy_test.go")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "fix: handle add\n\nCo-Authored-By: Claude <noreply@anthropic.com>")

	body := "Fixes #12\n\n## Testing\n- [x] Unit tests added/updated\n- [ ] Integration tests added/updated\n\n## Checklist\n- [x] Documentation updated\n"
	out, exit := runODSWithEnv(t, dir, []string{"ODS_PR_BODY=" + body}, "check", "--diff-base", "HEAD~1", "--json")
	if exit != 0 {
		t.Fatalf("check exit = %d, want 0 (these facts never deny by default)\n%s", exit, out)
	}
	var res struct {
		Allowed         bool     `json:"allowed"`
		Warnings        []string `json:"warnings"`
		ReviewTier      string   `json:"review_tier"`
		MergeConfidence struct {
			Claims []struct {
				Claim    string `json:"claim"`
				Verified bool   `json:"verified"`
				Evidence string `json:"evidence"`
			} `json:"claims"`
			TestsWeakened bool `json:"tests_weakened"`
			TestWeakening struct {
				DeletedTestFiles  []string `json:"deleted_test_files"`
				SkipsAdded        int      `json:"skips_added"`
				AssertionsRemoved int      `json:"assertions_removed"`
				AssertionsAdded   int      `json:"assertions_added"`
			} `json:"test_weakening"`
			SuppressionsAdded int `json:"suppressions_added"`
		} `json:"merge_confidence"`
	}
	mustJSON(t, out, &res)

	mc := res.MergeConfidence
	verified := map[string]bool{}
	for _, c := range mc.Claims {
		verified[c.Claim] = c.Verified
	}
	// A test file was modified, so the tests claim holds on its face; the
	// weakening facts are what show the modification removed checks.
	want := map[string]bool{"fixes-issue": true, "tests-added": true, "docs-updated": false}
	if len(verified) != len(want) {
		t.Fatalf("claims = %+v, want %v", mc.Claims, want)
	}
	for k, v := range want {
		if got, ok := verified[k]; !ok || got != v {
			t.Errorf("claim %s verified = %v (present %v), want %v", k, got, ok, v)
		}
	}
	if !mc.TestsWeakened {
		t.Error("tests_weakened = false, want true")
	}
	tw := mc.TestWeakening
	if len(tw.DeletedTestFiles) != 1 || tw.DeletedTestFiles[0] != "calc/legacy_test.go" {
		t.Errorf("deleted_test_files = %v, want [calc/legacy_test.go]", tw.DeletedTestFiles)
	}
	if tw.SkipsAdded != 1 || tw.AssertionsRemoved != 2 || tw.AssertionsAdded != 0 {
		t.Errorf("test_weakening = %+v, want 1 skip, 2 assertions removed, 0 added", tw)
	}
	if mc.SuppressionsAdded != 1 {
		t.Errorf("suppressions_added = %d, want 1", mc.SuppressionsAdded)
	}
	if !res.Allowed || res.ReviewTier != "elevated" {
		t.Errorf("allowed=%t review_tier=%q, want allowed and elevated", res.Allowed, res.ReviewTier)
	}
	for _, w := range []string{"documentation was updated", "Test file deleted (calc/legacy_test.go)", "1 test skip marker(s) added", "2 removed, 0 added", "1 lint or type-check suppression(s)"} {
		found := false
		for _, got := range res.Warnings {
			if strings.Contains(got, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("no warning containing %q in %v", w, res.Warnings)
		}
	}
}

// TestPipeline_ClaimsWithoutDescription: no description, no claims — and the
// test-weakening facts are still present with zero counts.
func TestPipeline_ClaimsWithoutDescription(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "a.go", "package a\n\nfunc A() int { return 1 }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: add a")

	out, _ := runODS(t, dir, "check", "--diff-base", "HEAD~1", "--json")
	var res struct {
		MergeConfidence map[string]any `json:"merge_confidence"`
	}
	mustJSON(t, out, &res)
	if _, ok := res.MergeConfidence["claims"]; ok {
		t.Errorf("claims present without a description: %v", res.MergeConfidence["claims"])
	}
	if res.MergeConfidence["tests_weakened"] != false {
		t.Errorf("tests_weakened = %v, want false", res.MergeConfidence["tests_weakened"])
	}
	if _, ok := res.MergeConfidence["test_weakening"]; !ok {
		t.Error("test_weakening missing, want zero counts")
	}
}

// TestPipeline_AgentTrace reads an Agent Trace record for the commit under
// review from .agent-trace/traces.jsonl: detection reports the agent-trace
// source with the measured lines, the score names it as the ratio's source,
// and the gate treats it as disclosed, corroborated attribution.
func TestPipeline_AgentTrace(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "svc/handler.go", `package svc

func A() int { return 1 }
func B() int { return 2 }
func C() int { return 3 }
func D() int { return 4 }
`)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: add handler") // no AI trailer on purpose

	head := gitOut(t, dir, "rev-parse", "HEAD")
	other := strings.Repeat("9", 40)
	writeFile(t, dir, ".agent-trace/traces.jsonl",
		`{"version":"0.1.0","id":"t1","timestamp":"2026-10-10T00:00:00Z","vcs":{"type":"git","revision":"`+head+`"},"tool":{"name":"claude-code"},"files":[{"path":"svc/handler.go","conversations":[{"url":"https://example.invalid/s/1","contributor":{"type":"ai","model_id":"anthropic/claude-opus-4-8"},"ranges":[{"start_line":3,"end_line":5}]}]}]}`+"\n"+
			`{"version":"0.1.0","id":"t2","timestamp":"2026-10-10T00:00:00Z","vcs":{"type":"git","revision":"`+other+`"},"files":[{"path":"svc/handler.go","conversations":[{"contributor":{"type":"ai"},"ranges":[{"start_line":1,"end_line":6}]}]}]}`+"\n")

	out, _ := runODS(t, dir, "detect", "--diff-base", "HEAD~1", "--branch", "main", "--json")
	var det struct {
		AIGenerated bool     `json:"ai_generated"`
		Sources     []string `json:"sources"`
		Files       []struct {
			Path    string `json:"path"`
			AILines int    `json:"ai_lines"`
		} `json:"files"`
		Evidence []struct {
			Source string `json:"source"`
			Value  string `json:"value"`
		} `json:"evidence"`
	}
	mustJSON(t, out, &det)
	if !det.AIGenerated || !contains(det.Sources, "agent-trace") {
		t.Fatalf("ai_generated=%t sources=%v, want agent-trace", det.AIGenerated, det.Sources)
	}
	if contains(det.Sources, "diff-heuristics") {
		t.Errorf("sources = %v: the heuristics must not run next to a measurement", det.Sources)
	}
	if len(det.Files) != 1 || det.Files[0].Path != "svc/handler.go" || det.Files[0].AILines != 3 {
		t.Errorf("files = %+v, want svc/handler.go with the 3 traced lines", det.Files)
	}
	found := false
	for _, ev := range det.Evidence {
		if ev.Source == "agent-trace" && strings.Contains(ev.Value, "3 AI line(s)") && strings.Contains(ev.Value, "claude-code/anthropic/claude-opus-4-8") {
			found = true
		}
	}
	if !found {
		t.Errorf("evidence = %+v, want an agent-trace row with the line count and agent", det.Evidence)
	}

	out, _ = runODS(t, dir, "score", "--diff-base", "HEAD~1", "--json")
	var sc struct {
		Breakdown struct {
			AICodeRatioSource string `json:"ai_code_ratio_source"`
		} `json:"breakdown"`
	}
	mustJSON(t, out, &sc)
	if sc.Breakdown.AICodeRatioSource != "agent-trace" {
		t.Errorf("ai_code_ratio_source = %q, want agent-trace", sc.Breakdown.AICodeRatioSource)
	}

	out, _ = runODS(t, dir, "check", "--diff-base", "HEAD~1", "--json")
	var chk struct {
		EvidenceTier string   `json:"evidence_tier"`
		Warnings     []string `json:"warnings"`
	}
	mustJSON(t, out, &chk)
	if chk.EvidenceTier != "corroborated" {
		t.Errorf("evidence_tier = %q, want corroborated", chk.EvidenceTier)
	}
	for _, w := range chk.Warnings {
		if strings.Contains(w, "without author disclosure") {
			t.Errorf("a traced change must count as disclosed, got warning %q", w)
		}
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = hermeticEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}
