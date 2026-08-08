// Package e2e contains black-box, end-to-end tests for the ods CLI.
//
// Unlike the per-package unit tests, these build the real ods binary and run the
// full detect → analyze → score → check pipeline as subprocesses against throwaway
// git fixtures. They exist to catch wiring regressions between the cmd layer and the
// detector/analyzer/scorer/policy engines that unit tests cannot see.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// odsBin is the path to the ods binary built once in TestMain.
var odsBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ods-e2e-bin")
	if err != nil {
		panic("creating temp bin dir: " + err.Error())
	}
	defer os.RemoveAll(dir)

	odsBin = filepath.Join(dir, "ods")
	build := exec.Command("go", "build", "-o", odsBin, "github.com/open-delivery-spec/cli/cmd/ods")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("building ods binary: " + err.Error())
	}

	os.Exit(m.Run())
}

// hermeticEnv returns a process environment with deterministic git identity and
// with any ODS_*/GITHUB_* variables stripped, so detection results depend only on
// the fixture repo and explicit flags — not on the host or CI environment.
func hermeticEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ODS_") || strings.HasPrefix(kv, "GITHUB_") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	return env
}

// git runs a git command in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = hermeticEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// runODS runs the ods binary in dir and returns stdout and the process exit code.
// Commands that report findings exit non-zero but still emit valid JSON on stdout,
// so callers parse stdout regardless of the exit code.
func runODS(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	stdout, _, exit := runODSStreams(t, dir, args...)
	return stdout, exit
}

// runODSStreams runs the ods binary and returns stdout, stderr, and the exit code.
func runODSStreams(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(odsBin, args...)
	cmd.Dir = dir
	cmd.Env = hermeticEnv()
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("running ods %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
		}
	}
	return stdout.String(), stderr.String(), exit
}

// writeFile writes a file relative to dir, creating parent directories.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// initRepo creates a git repo on branch `main` with one baseline commit so that
// HEAD~1 (the default diff base) always resolves.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	writeFile(t, dir, "README.md", "# fixture\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "chore: initial commit")
	return dir
}

// overCommentedGo is a Go source file whose comment-to-code ratio (~45%) reliably
// trips the analyzer's ai-over-commenting rule.
const overCommentedGo = `package widget

// Comment one explaining the package
// Comment two explaining the function
// Comment three explaining the math
// Comment four explaining the return
// Comment five explaining nothing
func Compute(x int) int {
	y := x + 1
	z := y * 2
	w := z - 3
	return w
}
`

func TestPipeline_HumanCode(t *testing.T) {
	dir := initRepo(t)
	// A second, plainly human commit with clean code and no AI markers.
	writeFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: add calc.Add")

	t.Run("detect reports no AI", func(t *testing.T) {
		out, exit := runODS(t, dir, "detect", "--branch", "main", "--json")
		if exit != 0 {
			t.Errorf("exit = %d, want 0 for human code", exit)
		}
		var res struct {
			AIGenerated bool `json:"ai_generated"`
		}
		mustJSON(t, out, &res)
		if res.AIGenerated {
			t.Errorf("ai_generated = true, want false for human code")
		}
	})

	t.Run("check allows the change", func(t *testing.T) {
		out, exit := runODS(t, dir, "check", "--json")
		if exit != 0 {
			t.Errorf("exit = %d, want 0 (policy should allow)", exit)
		}
		var res struct {
			Allowed bool `json:"allowed"`
		}
		mustJSON(t, out, &res)
		if !res.Allowed {
			t.Errorf("allowed = false, want true for clean human change")
		}
	})
}

func TestPipeline_AICode(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "widget.go", overCommentedGo)
	git(t, dir, "add", ".")
	// Co-Authored-By trailer is the primary AI attribution signal.
	git(t, dir, "commit", "-m", "feat: add widget\n\nCo-Authored-By: Claude <noreply@anthropic.com>")

	t.Run("detect reports AI from commit trailer", func(t *testing.T) {
		// High-confidence AI detection exits non-zero by design; JSON is still emitted.
		out, _ := runODS(t, dir, "detect", "--branch", "claude/widget", "--json")
		var res struct {
			AIGenerated bool     `json:"ai_generated"`
			Confidence  float64  `json:"confidence"`
			Sources     []string `json:"sources"`
		}
		mustJSON(t, out, &res)
		if !res.AIGenerated {
			t.Fatalf("ai_generated = false, want true")
		}
		if res.Confidence <= 0 {
			t.Errorf("confidence = %v, want > 0", res.Confidence)
		}
		if !contains(res.Sources, "commit-trailer") {
			t.Errorf("sources = %v, want to include commit-trailer", res.Sources)
		}
	})

	t.Run("analyze finds the over-commenting issue", func(t *testing.T) {
		out, _ := runODS(t, dir, "analyze", "--json")
		var res struct {
			Issues []struct {
				Rule     string `json:"rule"`
				Severity string `json:"severity"`
			} `json:"issues"`
		}
		mustJSON(t, out, &res)
		if !hasRule(res.Issues, "ai-over-commenting") {
			t.Errorf("expected ai-over-commenting issue, got %+v", res.Issues)
		}
	})

	t.Run("score emits a technical debt delta", func(t *testing.T) {
		out, _ := runODS(t, dir, "score", "--json")
		var res struct {
			TechnicalDebtDelta float64 `json:"technical_debt_delta"`
			Verdict            string  `json:"verdict"`
		}
		mustJSON(t, out, &res)
		if res.Verdict == "" {
			t.Errorf("verdict is empty, want a value")
		}
	})

	t.Run("check produces a policy decision", func(t *testing.T) {
		// Exit code depends on policy outcome; we only require valid, well-formed JSON
		// with the allowed field present.
		out, _ := runODS(t, dir, "check", "--json")
		var res map[string]json.RawMessage
		mustJSON(t, out, &res)
		if _, ok := res["allowed"]; !ok {
			t.Errorf("check JSON missing 'allowed' field: %s", out)
		}
	})
}

func TestDebugLogging(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "x.go", "package x\n\nfunc X() {}\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: add x")

	t.Run("no debug: stdout is JSON, stderr has no debug lines", func(t *testing.T) {
		stdout, stderr, _ := runODSStreams(t, dir, "detect", "--branch", "main", "--json")
		var res map[string]any
		mustJSON(t, stdout, &res)
		if strings.Contains(stderr, "[ods:debug]") {
			t.Errorf("stderr should have no debug output without --debug:\n%s", stderr)
		}
	})

	t.Run("--debug: stdout stays clean JSON, diagnostics go to stderr", func(t *testing.T) {
		stdout, stderr, _ := runODSStreams(t, dir, "detect", "--branch", "main", "--json", "--debug")
		// stdout must remain parseable JSON — debug must never leak into it.
		var res map[string]any
		mustJSON(t, stdout, &res)
		if strings.Contains(stdout, "[ods:debug]") {
			t.Errorf("debug output leaked into stdout:\n%s", stdout)
		}
		if !strings.Contains(stderr, "[ods:debug]") {
			t.Errorf("expected debug output on stderr, got:\n%s", stderr)
		}
	})

	t.Run("ODS_DEBUG env enables logging", func(t *testing.T) {
		cmd := exec.Command(odsBin, "detect", "--branch", "main", "--json")
		cmd.Dir = dir
		cmd.Env = append(hermeticEnv(), "ODS_DEBUG=1")
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		_ = cmd.Run()
		if !strings.Contains(stderr.String(), "[ods:debug]") {
			t.Errorf("ODS_DEBUG=1 should enable debug logging, stderr:\n%s", stderr.String())
		}
	})
}

// --- helpers ---

func mustJSON(t *testing.T, out string, v interface{}) {
	t.Helper()
	out = strings.TrimSpace(out)
	if out == "" {
		t.Fatalf("expected JSON output, got empty string")
	}
	if err := json.Unmarshal([]byte(out), v); err != nil {
		t.Fatalf("unmarshaling output: %v\noutput was:\n%s", err, out)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func hasRule(issues []struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
}, rule string) bool {
	for _, iss := range issues {
		if iss.Rule == rule {
			return true
		}
	}
	return false
}

// TestPipeline_CommitScanScopedToDiffBase guards against attribution leakage:
// AI trailers on commits *behind* the diff base belong to already-merged
// history and must not flag the change under review. It also checks that
// evidence rows carry the commit hash so multiple attributed commits render
// as distinguishable, auditable lines.
func TestPipeline_CommitScanScopedToDiffBase(t *testing.T) {
	dir := initRepo(t)

	// History: an already-merged AI-attributed commit…
	writeFile(t, dir, "old.go", "package old\n\nfunc Old() int { return 1 }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: old AI work\n\nCo-Authored-By: Claude <noreply@anthropic.com>")

	// …followed by the change under review: purely human.
	writeFile(t, dir, "new.go", "package new\n\nfunc New() int { return 2 }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: human follow-up")

	t.Run("human change is not flagged by AI history behind the base", func(t *testing.T) {
		out, exit := runODS(t, dir, "detect", "--diff-base", "HEAD~1", "--branch", "main", "--json")
		if exit != 0 {
			t.Errorf("exit = %d, want 0 for human change", exit)
		}
		var res struct {
			AIGenerated bool     `json:"ai_generated"`
			Sources     []string `json:"sources"`
		}
		mustJSON(t, out, &res)
		if res.AIGenerated {
			t.Errorf("ai_generated = true — AI commit behind the diff base leaked into the scan")
		}
		if contains(res.Sources, "commit-trailer") {
			t.Errorf("sources = %v — no commit-trailer evidence expected within HEAD~1..HEAD", res.Sources)
		}
	})

	t.Run("widening the base includes the AI commit with its hash", func(t *testing.T) {
		out, _ := runODS(t, dir, "detect", "--diff-base", "HEAD~2", "--branch", "main", "--json")
		var res struct {
			AIGenerated bool `json:"ai_generated"`
			Evidence    []struct {
				Source string `json:"source"`
				Value  string `json:"value"`
			} `json:"evidence"`
		}
		mustJSON(t, out, &res)
		if !res.AIGenerated {
			t.Fatalf("ai_generated = false, want true when the AI commit is in range")
		}
		found := false
		for _, ev := range res.Evidence {
			if ev.Source != "commit-trailer" {
				continue
			}
			found = true
			// Value shape: "AI-assisted commit <shorthash> (tool: Claude)"
			if !strings.Contains(ev.Value, "(tool: Claude)") || !strings.Contains(ev.Value, "AI-assisted commit ") {
				t.Errorf("evidence value = %q, want tool name and commit hash", ev.Value)
			}
			fields := strings.Fields(ev.Value)
			if len(fields) < 3 || len(fields[2]) < 7 {
				t.Errorf("evidence value = %q — expected a short hash as the third field", ev.Value)
			}
		}
		if !found {
			t.Error("no commit-trailer evidence found with the widened base")
		}
	})
}

// TestPipeline_KernelAssistedByTrailer verifies the Linux kernel's
// coding-assistants attribution convention end to end: a commit carrying
// "Assisted-by: AGENT:MODEL [tools...]" is attributed, with the agent and
// model surfaced in the evidence.
func TestPipeline_KernelAssistedByTrailer(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "vblank.go", "package drm\n\nfunc FixVblank() int { return 1 }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m",
		"fix: null deref in vblank handling\n\nAssisted-by: Claude:claude-3-opus coccinelle sparse")

	out, _ := runODS(t, dir, "detect", "--branch", "main", "--json")
	var res struct {
		AIGenerated bool     `json:"ai_generated"`
		Sources     []string `json:"sources"`
		Evidence    []struct {
			Source string `json:"source"`
			Value  string `json:"value"`
		} `json:"evidence"`
	}
	mustJSON(t, out, &res)
	if !res.AIGenerated {
		t.Fatalf("ai_generated = false, want true for Assisted-by trailer")
	}
	if !contains(res.Sources, "commit-trailer") {
		t.Errorf("sources = %v, want to include commit-trailer", res.Sources)
	}
	found := false
	for _, ev := range res.Evidence {
		if ev.Source == "commit-trailer" {
			found = true
			if !strings.Contains(ev.Value, "tool: Claude") || !strings.Contains(ev.Value, "model: claude-3-opus") {
				t.Errorf("evidence value = %q, want agent and model surfaced", ev.Value)
			}
		}
	}
	if !found {
		t.Error("no commit-trailer evidence for Assisted-by commit")
	}
}

// TestPipeline_GitAINotes verifies the git-ai integration end to end: a
// commit carrying a Git AI Standard v3 authorship log under refs/notes/ai is
// attributed with *measured* per-file AI lines, which replace the diff
// heuristics' estimates.
func TestPipeline_GitAINotes(t *testing.T) {
	dir := initRepo(t)
	// A 10-line code file; the note attributes 6 of its lines to an AI session.
	writeFile(t, dir, "svc/handler.go", `package svc

func A() int { return 1 }
func B() int { return 2 }
func C() int { return 3 }
func D() int { return 4 }
func E() int { return 5 }
func F() int { return 6 }
`)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: add handler") // no AI trailer on purpose

	note := `svc/handler.go
  s_c9883b05a2487d::t_9f8e7d6c5b4a32 3-8
---
{
  "schema_version": "authorship/3.0.0",
  "base_commit_sha": "0000000000000000000000000000000000000000",
  "prompts": {},
  "sessions": {
    "s_c9883b05a2487d": {
      "agent_id": {"tool": "cursor", "id": "abc", "model": "claude-sonnet-4-5"}
    }
  }
}`
	git(t, dir, "notes", "--ref=ai", "add", "-m", note, "HEAD")

	out, _ := runODS(t, dir, "detect", "--diff-base", "HEAD~1", "--branch", "main", "--json")
	var res struct {
		AIGenerated bool     `json:"ai_generated"`
		Sources     []string `json:"sources"`
		Files       []struct {
			Path       string  `json:"path"`
			AILines    int     `json:"ai_lines"`
			TotalLines int     `json:"total_lines"`
			Confidence float64 `json:"confidence"`
		} `json:"files"`
		Evidence []struct {
			Source string `json:"source"`
			Value  string `json:"value"`
		} `json:"evidence"`
	}
	mustJSON(t, out, &res)

	if !res.AIGenerated {
		t.Fatalf("ai_generated = false, want true from git-ai notes")
	}
	if !contains(res.Sources, "git-ai-notes") {
		t.Errorf("sources = %v, want to include git-ai-notes", res.Sources)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "svc/handler.go" {
		t.Fatalf("files = %+v, want exactly svc/handler.go", res.Files)
	}
	if res.Files[0].AILines != 6 {
		t.Errorf("ai_lines = %d, want the measured 6", res.Files[0].AILines)
	}
	if res.Files[0].TotalLines == 0 || res.Files[0].AILines > res.Files[0].TotalLines {
		t.Errorf("total_lines = %d, want > 0 and >= ai_lines", res.Files[0].TotalLines)
	}
	found := false
	for _, ev := range res.Evidence {
		if ev.Source == "git-ai-notes" {
			found = true
			if !strings.Contains(ev.Value, "6 AI line(s)") || !strings.Contains(ev.Value, "cursor/claude-sonnet-4-5") {
				t.Errorf("evidence value = %q, want line count and agent label", ev.Value)
			}
		}
	}
	if !found {
		t.Error("no git-ai-notes evidence emitted")
	}
}

// TestPipeline_GitAINotesAbsent locks the graceful path: repos without git-ai
// notes behave exactly as before (heuristics fallback, no new source).
func TestPipeline_GitAINotesAbsent(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "plain.go", "package plain\n\nfunc P() int { return 1 }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: plain change")

	out, _ := runODS(t, dir, "detect", "--diff-base", "HEAD~1", "--branch", "main", "--json")
	var res struct {
		Sources []string `json:"sources"`
	}
	mustJSON(t, out, &res)
	if contains(res.Sources, "git-ai-notes") {
		t.Errorf("sources = %v — git-ai-notes must not appear without notes", res.Sources)
	}
}

// TestAnalyze_PositionalFileArgs covers the entry point pre-commit uses: the
// analyzer is handed explicit staged filenames as positional args, analyzes
// the code ones (skipping non-code), and blocks (non-zero) on a critical
// finding.
func TestAnalyze_PositionalFileArgs(t *testing.T) {
	dir := initRepo(t)
	// A file with an unsafe-deserialization critical finding.
	writeFile(t, dir, "load.go", `package m
import "encoding/json"
func Load(b []byte) interface{} {
	var v interface{}
	json.Unmarshal(b, &v)
	return v
}
`)
	writeFile(t, dir, "notes.md", "# just docs\n")

	t.Run("analyzes code arg and surfaces the high finding", func(t *testing.T) {
		out, _ := runODS(t, dir, "analyze", "load.go", "--json")
		if !strings.Contains(out, "ai-unsafe-deserialization") {
			t.Errorf("expected the rule in output: %s", out)
		}
	})

	t.Run("--fail-on high blocks on a high finding (pre-commit gate)", func(t *testing.T) {
		_, exit := runODS(t, dir, "analyze", "load.go", "--fail-on", "high", "--json")
		if exit == 0 {
			t.Errorf("exit = 0, want non-zero with --fail-on high on a high finding")
		}
	})

	t.Run("default --fail-on critical does not block on a high finding", func(t *testing.T) {
		_, exit := runODS(t, dir, "analyze", "load.go", "--json")
		if exit != 0 {
			t.Errorf("exit = %d, want 0 (high < critical default threshold)", exit)
		}
	})

	t.Run("skips non-code args", func(t *testing.T) {
		out, exit := runODS(t, dir, "analyze", "notes.md", "--fail-on", "high", "--json")
		if exit != 0 {
			t.Errorf("exit = %d, want 0 when only non-code files are passed", exit)
		}
		if strings.Contains(out, "ai-unsafe-deserialization") {
			t.Errorf("non-code file should not be analyzed: %s", out)
		}
	})
}

// TestPipeline_AIReviewVerdict covers Gap 1 end to end: an AI reviewer's
// request_changes verdict routes the review tier to elevated without denying
// (probabilistic opinions tighten, never block, unless a policy opts in), and
// a verdict stamped for a different commit is skipped as stale.
// TestPipeline_DisclosureNudge covers disclosure completeness end to end:
// suspected AI without any author attribution draws a warning (the SFC and
// kernel docs both put the disclosure duty on the author), a Co-Authored-By
// trailer silences it, and the nudge never changes the exit code.
func TestPipeline_DisclosureNudge(t *testing.T) {
	runCheck := func(t *testing.T, dir, branch string) (allowed bool, warnings []string) {
		t.Helper()
		cmd := exec.Command(odsBin, "check", "--json")
		cmd.Dir = dir
		cmd.Env = append(hermeticEnv(), "ODS_BRANCH="+branch)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("check failed: %v\n%s", err, stderr.String())
		}
		var res struct {
			Allowed  bool     `json:"allowed"`
			Warnings []string `json:"warnings"`
		}
		mustJSON(t, stdout.String(), &res)
		return res.Allowed, res.Warnings
	}
	hasNudge := func(warnings []string) bool {
		for _, w := range warnings {
			if strings.Contains(w, "without author disclosure") {
				return true
			}
		}
		return false
	}

	t.Run("undisclosed suspicion warns without blocking", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "svc.go", "package svc\n\nfunc S() int { return 1 }\n")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "feat: add svc") // no attribution anywhere
		allowed, warnings := runCheck(t, dir, "copilot/add-svc")
		if !allowed {
			t.Errorf("disclosure nudge must never block, got allowed=false")
		}
		if !hasNudge(warnings) {
			t.Errorf("expected an author-disclosure warning, got %v", warnings)
		}
	})

	t.Run("trailer disclosure silences the nudge", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "svc.go", "package svc\n\nfunc S() int { return 1 }\n")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "feat: add svc\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
		allowed, warnings := runCheck(t, dir, "claude/add-svc")
		if !allowed {
			t.Errorf("disclosed AI change must stay allowed")
		}
		if hasNudge(warnings) {
			t.Errorf("disclosed change must not be nagged, got %v", warnings)
		}
	})
}

// TestPipeline_MergeConfidence covers the deterministic merge-confidence
// signals end to end: an AI-authored change that adds source without tests
// warns and routes to elevated (attribution raises the bar), it never denies,
// and adding a test clears the signal.
func TestPipeline_MergeConfidence(t *testing.T) {
	runCheck := func(t *testing.T, dir string) (allowed bool, tier string, warnings []string) {
		t.Helper()
		out, _, exit := runODSStreams(t, dir, "check", "--json")
		if exit != 0 {
			t.Fatalf("check exit = %d, want 0 (merge-confidence must not deny)\n%s", exit, out)
		}
		var res struct {
			Allowed    bool     `json:"allowed"`
			ReviewTier string   `json:"review_tier"`
			Warnings   []string `json:"warnings"`
		}
		mustJSON(t, out, &res)
		return res.Allowed, res.ReviewTier, res.Warnings
	}
	hasNoTestsWarn := func(warnings []string) bool {
		for _, w := range warnings {
			if strings.Contains(w, "no tests were added") {
				return true
			}
		}
		return false
	}

	t.Run("AI source without tests warns and elevates", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "svc.go", "package svc\n\nfunc Add(a, b int) int { return a + b }\n")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "feat: add svc\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
		allowed, tier, warnings := runCheck(t, dir)
		if !allowed {
			t.Error("merge-confidence must never deny by default")
		}
		if tier != "elevated" {
			t.Errorf("review_tier = %q, want elevated (AI source without tests)", tier)
		}
		if !hasNoTestsWarn(warnings) {
			t.Errorf("expected a no-tests warning, got %v", warnings)
		}
	})

	t.Run("adding a test clears the signal", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "svc.go", "package svc\n\nfunc Add(a, b int) int { return a + b }\n")
		writeFile(t, dir, "svc_test.go", "package svc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fail()\n\t}\n}\n")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "feat: add svc with tests\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
		allowed, tier, warnings := runCheck(t, dir)
		if !allowed {
			t.Error("tested change must stay allowed")
		}
		if tier == "elevated" {
			t.Errorf("tested change must not route elevated on the no-tests signal, got %q", tier)
		}
		if hasNoTestsWarn(warnings) {
			t.Errorf("tested change must not draw the no-tests warning, got %v", warnings)
		}
	})
}

// TestPipeline_PatchCoverage covers diff-scoped patch coverage end to end: an
// AI-authored change whose added lines are only partly covered by a Go
// coverage.out warns and routes to elevated (never denies); when the same added
// lines are fully covered, the signal clears. A test file is added in both cases
// so the no-tests signal is not what drives the routing — patch coverage is.
func TestPipeline_PatchCoverage(t *testing.T) {
	const svc = "package svc\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Sub(a, b int) int {\n\treturn a - b\n}\n"
	const svcTest = "package svc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fail()\n\t}\n}\n"

	runCheck := func(t *testing.T, dir string) (allowed bool, tier string, warnings []string) {
		t.Helper()
		out, _, exit := runODSStreams(t, dir, "check", "--json")
		if exit != 0 {
			t.Fatalf("check exit = %d, want 0 (patch coverage must not deny)\n%s", exit, out)
		}
		var res struct {
			Allowed       bool     `json:"allowed"`
			ReviewTier    string   `json:"review_tier"`
			Warnings      []string `json:"warnings"`
			PatchCoverage float64  `json:"patch_coverage"`
		}
		mustJSON(t, out, &res)
		return res.Allowed, res.ReviewTier, res.Warnings
	}
	hasPatchWarn := func(warnings []string) bool {
		for _, w := range warnings {
			if strings.Contains(w, "added lines are covered") {
				return true
			}
		}
		return false
	}

	t.Run("partly covered AI diff warns and elevates", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "svc.go", svc)
		writeFile(t, dir, "svc_test.go", svcTest)
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "feat: add svc\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
		// Add covered (lines 3-5), Sub uncovered (lines 7-9): 3/6 added lines → 50%.
		writeFile(t, dir, "coverage.out", "mode: set\nexample.com/m/svc.go:3.24,5.2 1 1\nexample.com/m/svc.go:7.24,9.2 1 0\n")
		allowed, tier, warnings := runCheck(t, dir)
		if !allowed {
			t.Error("patch coverage must never deny by default")
		}
		if tier != "elevated" {
			t.Errorf("review_tier = %q, want elevated (AI diff with low patch coverage)", tier)
		}
		if !hasPatchWarn(warnings) {
			t.Errorf("expected a patch-coverage warning, got %v", warnings)
		}
	})

	t.Run("fully covered AI diff clears the signal", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "svc.go", svc)
		writeFile(t, dir, "svc_test.go", svcTest)
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "feat: add svc\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
		// Both Add (3-5) and Sub (7-9) covered: 6/6 added lines → 100%.
		writeFile(t, dir, "coverage.out", "mode: set\nexample.com/m/svc.go:3.24,5.2 1 1\nexample.com/m/svc.go:7.24,9.2 1 1\n")
		allowed, tier, warnings := runCheck(t, dir)
		if !allowed {
			t.Error("fully covered change must stay allowed")
		}
		if tier == "elevated" {
			t.Errorf("fully covered change must not route elevated on patch coverage, got %q", tier)
		}
		if hasPatchWarn(warnings) {
			t.Errorf("fully covered change must not draw the patch-coverage warning, got %v", warnings)
		}
	})
}

// TestPipeline_MutationScore covers diff-scoped mutation score end to end: an
// AI-authored change whose added lines have surviving mutants (weak tests) warns
// and routes to elevated (never denies); when all mutants on those lines are
// killed, the signal clears. A gremlins report is supplied via --mutation; a
// test file is added so the no-tests signal is not what drives the routing.
func TestPipeline_MutationScore(t *testing.T) {
	const svc = "package svc\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Sub(a, b int) int {\n\treturn a - b\n}\n"
	const svcTest = "package svc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fail()\n\t}\n}\n"

	runCheck := func(t *testing.T, dir string) (allowed bool, tier string, warnings []string) {
		t.Helper()
		out, _, exit := runODSStreams(t, dir, "check", "--json", "--mutation", "gremlins.json")
		if exit != 0 {
			t.Fatalf("check exit = %d, want 0 (mutation score must not deny)\n%s", exit, out)
		}
		var res struct {
			Allowed       bool     `json:"allowed"`
			ReviewTier    string   `json:"review_tier"`
			Warnings      []string `json:"warnings"`
			MutationScore float64  `json:"mutation_score"`
		}
		mustJSON(t, out, &res)
		return res.Allowed, res.ReviewTier, res.Warnings
	}
	hasMutationWarn := func(warnings []string) bool {
		for _, w := range warnings {
			if strings.Contains(w, "kill only") {
				return true
			}
		}
		return false
	}

	t.Run("weak mutation score AI diff warns and elevates", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "svc.go", svc)
		writeFile(t, dir, "svc_test.go", svcTest)
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "feat: add svc\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
		// Both mutants on added return lines survive: 0/2 killed → 0% MSI.
		writeFile(t, dir, "gremlins.json", `{"files":[{"file_name":"svc.go","mutations":[{"line":4,"status":"LIVED"},{"line":8,"status":"LIVED"}]}]}`)
		allowed, tier, warnings := runCheck(t, dir)
		if !allowed {
			t.Error("mutation score must never deny by default")
		}
		if tier != "elevated" {
			t.Errorf("review_tier = %q, want elevated (AI diff with weak mutation score)", tier)
		}
		if !hasMutationWarn(warnings) {
			t.Errorf("expected a mutation-score warning, got %v", warnings)
		}
	})

	t.Run("strong mutation score AI diff clears the signal", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "svc.go", svc)
		writeFile(t, dir, "svc_test.go", svcTest)
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "feat: add svc\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
		// Both mutants on added lines are killed: 2/2 → 100% MSI.
		writeFile(t, dir, "gremlins.json", `{"files":[{"file_name":"svc.go","mutations":[{"line":4,"status":"KILLED"},{"line":8,"status":"KILLED"}]}]}`)
		allowed, tier, warnings := runCheck(t, dir)
		if !allowed {
			t.Error("strong mutation score change must stay allowed")
		}
		if tier == "elevated" {
			t.Errorf("strong mutation score must not route elevated, got %q", tier)
		}
		if hasMutationWarn(warnings) {
			t.Errorf("strong mutation score must not draw the mutation warning, got %v", warnings)
		}
	})
}

func TestPipeline_AIReviewVerdict(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "svc.go", "package svc\n\nfunc S() int { return 1 }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feat: add svc")

	writeVerdict := func(name, headSHA string) string {
		v := `{
  "schema": "ods.dev/review-verdict/v1",
  "reviewer": {"tool": "claude-code", "model": "claude-sonnet-4-5"},
  "head_sha": "` + headSHA + `",
  "verdict": "request_changes",
  "findings": [
    {"file": "svc.go", "line": 3, "severity": "high",
     "category": "correctness", "message": "S ignores its error path"}
  ]
}`
		writeFile(t, dir, name, v)
		return name
	}

	t.Run("request_changes elevates without denying", func(t *testing.T) {
		writeVerdict("review.json", "") // unstamped: applies to any head
		out, exit := runODS(t, dir, "check", "--ai-review", "review.json", "--json")
		if exit != 0 {
			t.Fatalf("exit = %d, want 0 — AI review must not deny by default\n%s", exit, out)
		}
		var res struct {
			Allowed    bool     `json:"allowed"`
			ReviewTier string   `json:"review_tier"`
			Warnings   []string `json:"warnings"`
		}
		mustJSON(t, out, &res)
		if !res.Allowed {
			t.Error("allowed = false, want true")
		}
		if res.ReviewTier != "elevated" {
			t.Errorf("review_tier = %q, want elevated", res.ReviewTier)
		}
	})

	t.Run("stale verdict for another commit is skipped", func(t *testing.T) {
		writeVerdict("stale.json", "0000000deadbeef")
		out, _, exit := runODSStreams(t, dir, "check", "--ai-review", "stale.json", "--json")
		if exit != 0 {
			t.Fatalf("exit = %d, want 0", exit)
		}
		var res struct {
			ReviewTier string `json:"review_tier"`
		}
		mustJSON(t, out, &res)
		if res.ReviewTier == "elevated" {
			t.Error("stale verdict must not influence routing")
		}
	})

	t.Run("ODS_HEAD_SHA overrides the checked-out HEAD", func(t *testing.T) {
		// CI checks out a synthetic merge commit on pull_request events, so a
		// verdict stamped with the PR head SHA never matches `git rev-parse
		// HEAD` there. ODS_HEAD_SHA tells check what to compare against.
		writeVerdict("stamped.json", "cafe1234")
		cmd := exec.Command(odsBin, "check", "--ai-review", "stamped.json", "--json")
		cmd.Dir = dir
		cmd.Env = append(hermeticEnv(), "ODS_HEAD_SHA=cafe1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("check failed: %v\n%s", err, stderr.String())
		}
		var res struct {
			ReviewTier string `json:"review_tier"`
		}
		mustJSON(t, stdout.String(), &res)
		if res.ReviewTier != "elevated" {
			t.Errorf("review_tier = %q, want elevated — stamped verdict must match ODS_HEAD_SHA, not repo HEAD", res.ReviewTier)
		}
	})
}

// TestPipeline_AnalyzeDocsOnly guards the benign-empty path: a diff that
// resolves but contains no analyzable code (docs-only PR) must yield a valid
// zero-issue JSON result at exit 0 — never the "no input provided" error — so
// CI wrappers can tell "nothing to analyze" apart from "the analyzer crashed".
func TestPipeline_AnalyzeDocsOnly(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "docs/guide.md", "# guide\n\nprose only\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "docs: add guide")

	out, exit := runODS(t, dir, "analyze", "--json")
	if exit != 0 {
		t.Fatalf("analyze on a docs-only diff must exit 0, got %d\n%s", exit, out)
	}
	var res struct {
		Issues  []any  `json:"issues"`
		Summary string `json:"summary"`
	}
	mustJSON(t, out, &res)
	if len(res.Issues) != 0 {
		t.Errorf("expected no issues, got %v", res.Issues)
	}
	if !strings.Contains(res.Summary, "No analyzable code") {
		t.Errorf("summary = %q, want the explicit no-code summary", res.Summary)
	}
}
