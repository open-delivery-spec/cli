package detector

import (
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

func TestParseCommitMessage_aiFooter(t *testing.T) {
	msg := `feat(auth): add OAuth login

Added full OAuth flow for user authentication.

AI-assisted: true
AI-tool: GitHub Copilot`

	ev := ParseCommitMessage(msg)
	if len(ev) == 0 {
		t.Fatal("ParseCommitMessage returned no evidence")
	}
	hasFooter := false
	hasTool := false
	for _, e := range ev {
		if e.Signal == "ai-footer" {
			hasFooter = true
		}
		if e.Signal == "ai-tool" {
			hasTool = true
		}
	}
	if !hasFooter {
		t.Error("missing ai-footer signal")
	}
	if !hasTool {
		t.Error("missing ai-tool signal")
	}
}

func TestParseCommitMessage_noAI(t *testing.T) {
	msg := "fix(ui): correct button alignment"
	ev := ParseCommitMessage(msg)
	if len(ev) != 0 {
		t.Errorf("ParseCommitMessage = %d evidence, want 0", len(ev))
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
		got := isCodeFile(tt.path)
		if got != tt.want {
			t.Errorf("isCodeFile(%s) = %v, want %v", tt.path, got, tt.want)
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
}

func TestHeuristicScores(t *testing.T) {
	lines := []string{
		"// Process the data",
		"func processData(data []byte) error {",
		"    if err != nil { return err }",
		"    if err != nil { return err }",
		"    return nil",
		"}",
	}
	scores := HeuristicScores(lines)
	expected := []string{"comment-ratio", "verbose-naming", "redundant-error-handling", "uniform-indent"}
	for _, name := range expected {
		if _, ok := scores[name]; !ok {
			t.Errorf("HeuristicScores missing key: %s", name)
		}
	}
}

func TestIsHighConfidence(t *testing.T) {
	r := &DetectionResult{Confidence: 0.85}
	if !r.IsHighConfidence() {
		t.Error("IsHighConfidence should return true at 0.85")
	}
	r.Confidence = 0.7
	if r.IsHighConfidence() {
		t.Error("IsHighConfidence should return false at 0.7")
	}
}

func TestIsLowConfidence(t *testing.T) {
	r := &DetectionResult{Confidence: 0.3}
	if !r.IsLowConfidence() {
		t.Error("IsLowConfidence should return true at 0.3")
	}
	r.Confidence = 0.5
	if r.IsLowConfidence() {
		t.Error("IsLowConfidence should return false at 0.5")
	}
}

func TestFileDetection_helpers(t *testing.T) {
	files := []FileDetection{
		{Path: "a.go", AILines: 10, TotalLines: 50, Confidence: 0.6},
		{Path: "b.go", AILines: 20, TotalLines: 30, Confidence: 0.8},
		{Path: "c.go", AILines: 0, TotalLines: 40, Confidence: 0.2},
	}
	if CountAIFiles(files) != 2 {
		t.Errorf("CountAIFiles = %d, want 2", CountAIFiles(files))
	}
	// totalAI = 10+20+0=30, totalAll = 50+30+40=120
	summary := FormatDiffLineCount(files)
	if !strings.Contains(summary, "30/120") {
		t.Errorf("FormatDiffLineCount = %s, want containing '30/120'", summary)
	}
}

func TestNonEmptyLines(t *testing.T) {
	result := nonEmptyLines("a\n\nb\n  \nc\n")
	if len(result) != 3 {
		t.Fatalf("nonEmptyLines = %d, want 3", len(result))
	}
}

func TestSplitCommits(t *testing.T) {
	raw := "feat(auth): add OAuth\n\nAI-assisted: true\n\nfix(ui): button\n\nrefactor(core): clean up"
	commits := splitCommits(raw)
	// Should find at least one commit
	if len(commits) == 0 {
		t.Fatal("splitCommits returned no commits")
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AITrailerTool(c.msg); got != c.want {
				t.Errorf("AITrailerTool(%q) = %q, want %q", c.msg, got, c.want)
			}
		})
	}
}
