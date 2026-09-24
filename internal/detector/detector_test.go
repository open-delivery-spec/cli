package detector

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectFromBranch_ai(t *testing.T) {
	ev := detectFromBranch("ai-add-auth-feature")
	if ev == nil {
		t.Fatal("detectFromBranch('ai-add-auth-feature') returned nil, want evidence")
	}
	if ev.Signal != "ai-prefix" {
		t.Errorf("signal = %s, want ai-prefix", ev.Signal)
	}
	if ev.Confidence != 0.5 {
		t.Errorf("confidence = %f, want 0.5", ev.Confidence)
	}
}

func TestDetectFromBranch_segment(t *testing.T) {
	ev := detectFromBranch("feature/ai-login-fix")
	if ev == nil || ev.Signal != "ai-prefix-segment" {
		t.Fatal("detectFromBranch('feature/ai-login-fix') should detect AI segment")
	}
}

func TestDetectFromBranch_normal(t *testing.T) {
	ev := detectFromBranch("feature/add-login")
	if ev != nil {
		t.Errorf("detectFromBranch('feature/add-login') = %v, want nil", ev)
	}
}

func TestDetectFromPRBody_checkbox(t *testing.T) {
	body := `## AI Disclosure
- [x] This PR contains AI-generated code
- **AI Tool:** GitHub Copilot`

	ev := detectFromPRBody(body)
	if ev == nil {
		t.Fatal("detectFromPRBody with checked box returned nil")
	}
	if ev.Signal != "ai-disclosure-checkbox" {
		t.Errorf("signal = %s, want ai-disclosure-checkbox", ev.Signal)
	}
}

func TestDetectFromPRBody_text(t *testing.T) {
	body := `## AI Disclosure
This change includes AI-generated code for the auth module.
AI Tool: Claude`

	ev := detectFromPRBody(body)
	if ev == nil {
		t.Fatal("detectFromPRBody with AI text returned nil")
	}
	if ev.Confidence < 0.7 {
		t.Errorf("confidence = %f, want >= 0.7", ev.Confidence)
	}
}

func TestDetectFromPRBody_empty(t *testing.T) {
	ev := detectFromPRBody("")
	if ev != nil {
		t.Errorf("detectFromPRBody('') = %v, want nil", ev)
	}
}

func TestDetectFromPRBody_noAI(t *testing.T) {
	ev := detectFromPRBody("## Summary\nAdded a new feature\n## Changes\n- login button")
	if ev != nil {
		t.Errorf("detectFromPRBody with no AI content = %v, want nil", ev)
	}
}

func TestCommentRatio_high(t *testing.T) {
	lines := []string{
		"// This function handles user authentication",
		"// It validates the token and checks expiry",
		"// Returns an error if anything fails",
		"// We also log the attempt for audit purposes",
		"// This is important for security compliance",
		"func handleAuth(token string) error {",
		"    if token == \"\" {",
		"        return errors.New(\"empty token\")",
		"    }",
		"}",
	}
	score := commentRatio(lines)
	if score < 0.5 {
		t.Errorf("commentRatio = %f, want >= 0.5 for highly-commented code", score)
	}
}

func TestCommentRatio_low(t *testing.T) {
	lines := []string{
		"func add(a, b int) int {",
		"    return a + b",
		"}",
		"func sub(a, b int) int {",
		"    return a - b",
		"}",
	}
	score := commentRatio(lines)
	if score > 0.2 {
		t.Errorf("commentRatio = %f, want <= 0.2 for minimal comments", score)
	}
}

func TestVerboseNamingScore(t *testing.T) {
	lines := []string{
		"userAuthenticationTokenValidationHandler := func(w http.ResponseWriter, r *http.Request) {",
		"    temporaryResponseBufferVariable := make([]byte, 1024)",
		"    x := 1",
		"    y := 2",
	}
	score := verboseNamingScore(lines)
	if score < 0.3 {
		t.Errorf("verboseNamingScore = %f, want >= 0.3 for long names", score)
	}
}

func TestRedundantErrorHandlingScore(t *testing.T) {
	lines := []string{
		"if err != nil {",
		"    return err",
		"}",
		"data, err := fetchData()",
		"if err != nil {",
		"    log.Printf(\"error: %v\", err)",
		"    return err",
		"}",
		"result, err := process(data)",
		"if err != nil {",
		"    return err",
		"}",
	}
	score := redundantErrorHandlingScore(lines)
	if score < 0.2 {
		t.Errorf("redundantErrorHandlingScore = %f, want >= 0.2", score)
	}
}

func TestUniformIndentScore(t *testing.T) {
	lines := []string{
		"    x := 1",
		"    y := 2",
		"    z := 3",
		"    result := x + y + z",
		"    fmt.Println(result)",
		"    if result > 0 {",
		"        fmt.Println(\"positive\")",
		"    }",
	}
	score := uniformIndentScore(lines)
	// All 4-space indented lines: high uniformity
	if score < 0.3 {
		t.Errorf("uniformIndentScore = %f, want >= 0.3", score)
	}
}

func TestScoreAIPatterns(t *testing.T) {
	// Simulate AI-like code: high comments, verbose naming, redundant err checks
	lines := []string{
		"// This function processes user authentication requests",
		"// It validates the request parameters and returns a token",
		"// The token is used for subsequent API calls",
		"// We store the token in a secure HTTP-only cookie",
		"func processUserAuthenticationRequest(requestParameters interface{}) error {",
		"    if err != nil {",
		"        log.Printf(\"authentication error occurred: %v\", err)",
		"        return fmt.Errorf(\"authentication processing failed: %w\", err)",
		"    }",
		"    if err != nil {",
		"        return err",
		"    }",
		"    if err != nil {",
		"        return err",
		"    }",
		"    return nil",
		"}",
	}
	score := scoreAIPatterns(lines)
	// With 4 comment lines + verbose function name + 3 error blocks
	// the weighted heuristic score should indicate elevated AI likelihood (>0.1)
	if score < 0.1 {
		t.Errorf("scoreAIPatterns = %f, want >= 0.1 for AI-like code", score)
	}
}

func TestScoreAIPatterns_humanCode(t *testing.T) {
	lines := []string{
		"func add(a, b int) int { return a + b }",
		"func sub(a, b int) int { return a - b }",
	}
	score := scoreAIPatterns(lines)
	if score > 0.3 {
		t.Errorf("scoreAIPatterns = %f, want <= 0.3 for terse code", score)
	}
}

func TestExtractAddedLines(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index 123..456 789
--- a/main.go
+++ b/main.go
@@ -1,3 +1,5 @@
 unchanged line
+added line 1
+added line 2
 unchanged line 2`

	lines := extractAddedLines(diff)
	if len(lines) != 2 {
		t.Fatalf("extractAddedLines = %d lines, want 2", len(lines))
	}
	if lines[0] != "added line 1" {
		t.Errorf("line[0] = %s, want 'added line 1'", lines[0])
	}
}

func TestIsCodeFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"lib.rs", true},
		{"app.py", true},
		{"index.js", true},
		{"Component.tsx", true},
		{"App.kt", true},
		{"Program.cs", true},
		{"README.md", false},
		{"Dockerfile", false},
		{"Makefile", false},
		{"test_config.yaml", false},
		{"image.png", false},
	}
	for _, tt := range tests {
		got := IsCodeFile(tt.path)
		if got != tt.want {
			t.Errorf("IsCodeFile(%s) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestDetectionResult_aggregate(t *testing.T) {
	t.Run("no evidence", func(t *testing.T) {
		r := &DetectionResult{}
		r.aggregate()
		if r.AIGenerated {
			t.Error("empty result should not detect AI")
		}
		if r.Confidence != 0.0 {
			t.Errorf("confidence = %f, want 0.0", r.Confidence)
		}
	})

	t.Run("strong commit evidence", func(t *testing.T) {
		r := &DetectionResult{
			Evidence: []Evidence{
				{Source: "commit-trailer", Signal: "ai-footer", Confidence: 0.9},
			},
		}
		r.aggregate()
		if !r.AIGenerated {
			t.Error("should detect AI with commit trailer evidence")
		}
		if r.Confidence < 0.8 {
			t.Errorf("confidence = %f, want >= 0.8", r.Confidence)
		}
	})

	t.Run("weak branch evidence alone", func(t *testing.T) {
		r := &DetectionResult{
			Evidence: []Evidence{
				{Source: "branch-name", Signal: "ai-prefix", Confidence: 0.5},
			},
		}
		r.aggregate()
		if !r.AIGenerated {
			t.Error("should detect AI with branch evidence alone")
		}
	})

	t.Run("composite evidence", func(t *testing.T) {
		r := &DetectionResult{
			Evidence: []Evidence{
				{Source: "commit-trailer", Signal: "ai-footer", Confidence: 0.9},
				{Source: "pr-body", Signal: "ai-disclosure-checkbox", Confidence: 0.85},
			},
			Files: []FileDetection{
				{Path: "auth.go", AILines: 30, TotalLines: 80, Confidence: 0.6},
			},
		}
		r.aggregate()
		if !r.AIGenerated {
			t.Error("should detect AI with composite evidence")
		}
		if r.Confidence < 0.7 {
			t.Errorf("confidence = %f, want >= 0.7", r.Confidence)
		}
	})

	t.Run("a second source corroborates", func(t *testing.T) {
		r := &DetectionResult{
			Evidence: []Evidence{
				{Source: "commit-trailer", Signal: "ai-footer", Confidence: 0.9},
				{Source: "pr-body", Signal: "ai-disclosure-checkbox", Confidence: 0.85},
			},
		}
		r.aggregate()
		if math.Abs(r.Confidence-0.95) > 1e-9 {
			t.Errorf("confidence = %f, want 0.95 (0.9 + one corroborating source)", r.Confidence)
		}
	})

	t.Run("many commits from one source do not stack toward certainty", func(t *testing.T) {
		// Five attributed commits are one source saying the same thing. Before
		// the fix each one added 5%, and any multi-commit AI change reported
		// 100% — a certainty ODS explicitly does not claim.
		var ev []Evidence
		for i := 0; i < 5; i++ {
			ev = append(ev, Evidence{Source: "commit-trailer", Signal: "ai-footer", Confidence: 0.9})
		}
		r := &DetectionResult{Evidence: ev}
		r.aggregate()
		if math.Abs(r.Confidence-0.9) > 1e-9 {
			t.Errorf("confidence = %f, want 0.9 (one source, however many commits)", r.Confidence)
		}
	})

	t.Run("never reports certainty", func(t *testing.T) {
		r := &DetectionResult{
			Evidence: []Evidence{
				{Source: "git-ai-notes", Signal: "authorship-log", Confidence: 0.95},
				{Source: "commit-trailer", Signal: "ai-footer", Confidence: 0.9},
				{Source: "pr-body", Signal: "ai-disclosure-checkbox", Confidence: 0.85},
				{Source: "branch-name", Signal: "ai-tool-branch", Confidence: 0.6},
			},
			Files: []FileDetection{{Path: "a.go", AILines: 10, TotalLines: 10, Confidence: 0.95}},
		}
		r.aggregate()
		if r.Confidence > MaxConfidence {
			t.Errorf("confidence = %f, want <= %v: attribution is volunteered, never proven", r.Confidence, MaxConfidence)
		}
		if r.Confidence < 0.95 {
			t.Errorf("confidence = %f, want the cap when every source agrees", r.Confidence)
		}
	})
}

// gitIn runs a git command in dir with a fixed identity and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeIn(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestFilesFromCommits covers the attested per-file AI line counts: the lines
// AI-attributed commits added to code files, capped at what the change still
// contains, with human commits and non-code files left out.
func TestFilesFromCommits(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	writeIn(t, dir, "README.md", "# fixture\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "chore: init")
	base := gitIn(t, dir, "rev-parse", "HEAD")

	// A human commit adds code that must not be attributed to AI.
	writeIn(t, dir, "human.go", "package p\n\nfunc A() {}\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: human part")

	// An AI-attributed commit adds five code lines and a docs file.
	writeIn(t, dir, "ai.go", "package p\n\nfunc B() {}\n\nfunc C() {}\n")
	writeIn(t, dir, "notes.md", "one\ntwo\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: ai part\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
	aiHash := gitIn(t, dir, "rev-parse", "--short", "HEAD")

	t.Chdir(dir)

	t.Run("counts only the code lines the attributed commit added", func(t *testing.T) {
		files := filesFromCommits([]string{aiHash}, base)
		if len(files) != 1 || files[0].Path != "ai.go" {
			t.Fatalf("files = %+v, want only ai.go", files)
		}
		if files[0].AILines != 5 || files[0].TotalLines != 5 {
			t.Errorf("ai.go = %d/%d lines, want 5/5", files[0].AILines, files[0].TotalLines)
		}
		if files[0].Confidence != 0.9 {
			t.Errorf("confidence = %v, want the trailer's 0.9 (attested, not measured)", files[0].Confidence)
		}
	})

	t.Run("detect prefers attested counts over heuristics", func(t *testing.T) {
		res, err := Detect(Options{DiffBase: base, MaxCommits: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Files) != 1 || res.Files[0].Path != "ai.go" || res.Files[0].AILines != 5 {
			t.Errorf("files = %+v, want ai.go with 5 attested AI lines", res.Files)
		}
		for _, s := range res.Sources {
			if s == "diff-heuristics" {
				t.Errorf("sources = %v: heuristics must not run next to a trailer", res.Sources)
			}
		}
		if res.Confidence > MaxConfidence {
			t.Errorf("confidence = %v, want <= %v", res.Confidence, MaxConfidence)
		}
	})

	t.Run("caps at what the change still contains", func(t *testing.T) {
		// A later human commit trims the AI file to two lines: the range diff
		// adds two, so the attributed count cannot exceed two.
		writeIn(t, dir, "ai.go", "package p\n\nfunc B() {}\n")
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-q", "-m", "refactor: trim")
		files := filesFromCommits([]string{aiHash}, base)
		if len(files) != 1 || files[0].AILines != 3 || files[0].TotalLines != 3 {
			t.Errorf("files = %+v, want ai.go capped at 3/3", files)
		}
	})
}

func TestNonEmptyLines(t *testing.T) {
	result := nonEmptyLines("a\n\nb\n  \nc\n")
	if len(result) != 3 {
		t.Fatalf("nonEmptyLines = %d, want 3", len(result))
	}
}

func TestAITrailerTool(t *testing.T) {
	cases := []struct {
		name, msg, want string
	}{
		{"claude co-author", "feat: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>", "Claude"},
		{"copilot co-author", "fix: y\n\nCo-Authored-By: GitHub Copilot <copilot@github.com>", "GitHub Copilot"},
		{"ai-tool trailer", "chore: z\n\nAI-tool: Cursor", "Cursor"},
		{"ai-assisted bare", "docs: d\n\nAI-assisted: true", "AI"},
		{"human", "feat: human change\n\nCo-Authored-By: Jane Dev <jane@example.com>", ""},
		{"empty", "", ""},
		// One tool, many spellings: reports aggregate under one name.
		{"claude model co-author", "feat: x\n\nCo-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>", "Claude"},
		{"claude sonnet co-author", "feat: x\n\nCo-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>", "Claude"},
		{"copilot agent co-author", "fix: y\n\nCo-Authored-By: copilot-swe-agent[bot] <198982749+Copilot@users.noreply.github.com>", "GitHub Copilot"},
		{"lowercase copilot co-author", "fix: y\n\nCo-Authored-By: copilot <copilot@github.com>", "GitHub Copilot"},
		{"ai-tool claude code", "chore: z\n\nAI-tool: Claude Code", "Claude"},
		{"ai-tool unknown stays as written", "chore: z\n\nAI-tool: Amp", "Amp"},
		// Trailers other tools and projects emit.
		{"codex co-author", "feat: x\n\nCo-authored-by: Codex <noreply@openai.com>", "Codex"},
		{"gemini co-author", "feat: x\n\nCo-authored-by: Gemini <gemini-code-assist@google.com>", "Gemini"},
		{"cursor made-with", "feat: x\n\nMade-with: Cursor", "Cursor"},
		{"asf generated-by", "feat: x\n\nGenerated-by: GitHub Copilot", "GitHub Copilot"},
		{"claude session trailer", "feat: x\n\nClaude-Session: https://claude.ai/code/session_01AbC", "Claude"},
		{"qemu ai-used-for", "feat: x\n\nAI-used-for: tests", "AI"},
		// Code generators use Generated-by too; only a known AI tool counts.
		{"generated-by code generator", "chore: regen\n\nGenerated-by: protoc-gen-go", ""},
		{"made-with not a tool", "docs: x\n\nMade-with: love", ""},
		{"assisted-by lowercase agent", "fix: q\n\nAssisted-by: claude:claude-sonnet-4-6", "Claude"},
		// A human whose name starts like a tool name is not the tool.
		{"human aiden", "feat: x\n\nCo-Authored-By: Aiden Smith <aiden@example.com>", ""},
		{"human claudette", "feat: x\n\nCo-Authored-By: Claudette Roy <claudette@example.com>", ""},
		{"ai co-author after human", "feat: x\n\nCo-Authored-By: Jane Dev <jane@example.com>\nCo-Authored-By: Claude <noreply@anthropic.com>", "Claude"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AITrailerTool(c.msg); got != c.want {
				t.Errorf("AITrailerTool(%q) = %q, want %q", c.msg, got, c.want)
			}
		})
	}
}

// ── Assisted-by (Linux kernel coding-assistants convention) ──────────────────

func TestParseAssistedBy(t *testing.T) {
	cases := []struct {
		line         string
		agent, model string
		ok           bool
	}{
		{"Assisted-by: Claude:claude-3-opus", "Claude", "claude-3-opus", true},
		{"Assisted-by: Claude:claude-3-opus coccinelle sparse", "Claude", "claude-3-opus", true},
		{"Assisted-by: Claude", "Claude", "", true}, // bare agent, no model
		{"assisted-by: copilot:gpt-4o", "copilot", "gpt-4o", true},
		{"  Assisted-by: Gemini:gemini-pro clang-tidy", "Gemini", "gemini-pro", true},
		{"Assisted-by:", "", "", false},
		{"Co-Authored-By: Claude <noreply@anthropic.com>", "", "", false},
		{"Not-Assisted-by: Claude", "", "", false},
	}
	for _, tc := range cases {
		agent, model, ok := parseAssistedBy(tc.line)
		if agent != tc.agent || model != tc.model || ok != tc.ok {
			t.Errorf("parseAssistedBy(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.line, agent, model, ok, tc.agent, tc.model, tc.ok)
		}
	}
}

func TestAITrailerToolAssistedBy(t *testing.T) {
	msg := "drm: fix null deref in vblank handling\n\nAssisted-by: Claude:claude-3-opus coccinelle\nSigned-off-by: Dev <dev@example.com>"
	if got := AITrailerTool(msg); got != "Claude" {
		t.Errorf("AITrailerTool = %q, want %q (aggregate by agent name)", got, "Claude")
	}
	human := "drm: fix null deref\n\nSigned-off-by: Dev <dev@example.com>"
	if got := AITrailerTool(human); got != "" {
		t.Errorf("AITrailerTool = %q, want empty for human commit", got)
	}
}

func TestEvidenceTier(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
		want    string
	}{
		{"none", nil, ""},
		{"git-ai-notes is corroborated", []string{"git-ai-notes"}, "corroborated"},
		{"commit-trailer is attested", []string{"commit-trailer"}, "attested"},
		{"pr-body is attested", []string{"pr-body"}, "attested"},
		{"branch-name is inferred", []string{"branch-name"}, "inferred"},
		{"diff-heuristics is inferred", []string{"diff-heuristics"}, "inferred"},
		{"highest present wins: trailer beats branch", []string{"branch-name", "commit-trailer"}, "attested"},
		{"highest present wins: notes beat trailer", []string{"commit-trailer", "git-ai-notes"}, "corroborated"},
		{"unknown source ignored", []string{"mystery"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvidenceTier(tc.sources); got != tc.want {
				t.Errorf("EvidenceTier(%v) = %q, want %q", tc.sources, got, tc.want)
			}
		})
	}
}

// TestDetectFromCommits_OtherTrailers covers attribution trailers written by
// tools and projects other than the Co-Authored-By default: Cursor's
// Made-with, the ASF's Generated-by, Claude Code's session link and QEMU's
// AI-used-for, which also records what the AI was used for.
func TestDetectFromCommits_OtherTrailers(t *testing.T) {
	cases := []struct {
		name, msg string
		wantAI    bool
		wantValue string
	}{
		{"made-with cursor", "feat: x\n\nMade-with: Cursor\n", true, "tool: Cursor"},
		{"generated-by", "feat: x\n\nGenerated-by: Claude Code\n", true, "tool: Claude"},
		{"claude session", "feat: x\n\nClaude-Session: https://claude.ai/code/session_01AbC\n", true, "tool: Claude"},
		{"ai-used-for", "feat: x\n\nAI-used-for: code, tests\n", true, "[scope: code, tests]"},
		{"generated-by generator", "chore: x\n\nGenerated-by: stringer\n", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir) // not a git repository: the message file is scanned
			msgFile := filepath.Join(dir, "COMMIT_EDITMSG")
			if err := os.WriteFile(msgFile, []byte(c.msg), 0o644); err != nil {
				t.Fatal(err)
			}
			ev, _ := detectFromCommits(Options{CommitMessageFile: msgFile, MaxCommits: 1})
			if got := len(ev) > 0; got != c.wantAI {
				t.Fatalf("AI attributed = %t, want %t (evidence %v)", got, c.wantAI, ev)
			}
			if c.wantAI && !strings.Contains(ev[0].Value, c.wantValue) {
				t.Errorf("evidence %q should contain %q", ev[0].Value, c.wantValue)
			}
		})
	}
}

func TestDetectFromBranch_codex(t *testing.T) {
	if ev := detectFromBranch("codex/fix-flaky-test"); ev == nil {
		t.Fatal("detectFromBranch('codex/fix-flaky-test') returned nil, want evidence")
	}
}
