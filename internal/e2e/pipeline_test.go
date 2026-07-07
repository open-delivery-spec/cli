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
