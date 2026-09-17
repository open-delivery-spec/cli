package analyzer

import (
	"strings"
	"testing"
)

func TestCheckRedundantErrorHandling(t *testing.T) {
	t.Run("consecutive simple returns", func(t *testing.T) {
		lines := []string{
			"data, err := fetchData()",
			"if err != nil {",
			"    return err",
			"}",
			"result, err := process(data)",
			"if err != nil {",
			"    return err",
			"}",
		}
		issues := checkRedundantErrorHandling("test.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected redundant error handling issue, got none")
		}
		if issues[0].Rule != "ai-redundant-error-handling" {
			t.Errorf("rule = %s, want ai-redundant-error-handling", issues[0].Rule)
		}
	})

	t.Run("consecutive blocks with different handling", func(t *testing.T) {
		lines := []string{
			"data, err := fetchData()",
			"if err != nil {",
			"    log.Printf(\"fetch error: %v\", err)",
			"    return fmt.Errorf(\"fetch: %w\", err)",
			"}",
			"result, err := process(data)",
			"if err != nil {",
			"    return err",
			"}",
		}
		issues := checkRedundantErrorHandling("test.go", lines)
		// First block wraps error, second just returns — same pattern detected
		if len(issues) == 0 {
			t.Fatal("expected redundant error handling issue for wrapped+simple pattern")
		}
	})

	t.Run("no redundant pattern", func(t *testing.T) {
		lines := []string{
			"data, err := fetchData()",
			"if err != nil {",
			"    return err",
			"}",
			"x := 1",
			"y := 2",
			"z := 3",
			"w := 4",
			"v := 5",
			"u := 6",
			"result, err := process(data)",
			"if err != nil {",
			"    return err",
			"}",
		}
		issues := checkRedundantErrorHandling("test.go", lines)
		if len(issues) > 0 {
			t.Errorf("expected no issues for spaced-out error blocks, got %d", len(issues))
		}
	})
}

func TestCheckOverCommenting(t *testing.T) {
	t.Run("high comment ratio", func(t *testing.T) {
		lines := []string{
			"// This function handles user authentication",
			"// It validates the token and checks expiry",
			"// Returns an error if authentication fails",
			"// We log all authentication attempts",
			"// This is important for audit compliance",
			"// Session tokens are stored in Redis for fast lookup",
			"func auth(token string) error {",
			"    if token == \"\" {",
			"        return errors.New(\"empty\")",
			"    }",
			"    return nil",
			"}",
		}
		issues := checkOverCommenting("test.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected over-commenting issue, got none")
		}
		// Over-commenting is a style hint, always informational — it must never
		// be promoted to a blocking severity (regression guard).
		if issues[0].Severity != "info" {
			t.Errorf("severity = %s, want info", issues[0].Severity)
		}
	})

	t.Run("normal comment ratio", func(t *testing.T) {
		lines := []string{
			"func add(a, b int) int {",
			"    return a + b",
			"}",
			"func sub(a, b int) int {",
			"    return a - b",
			"}",
			"func mul(a, b int) int {",
			"    return a * b",
			"}",
		}
		issues := checkOverCommenting("test.go", lines)
		if len(issues) > 0 {
			t.Errorf("expected no issues for normal code, got %d", len(issues))
		}
	})
}

func TestCheckUnsafeDeserialization(t *testing.T) {
	t.Run("unsafe unmarshal", func(t *testing.T) {
		lines := []string{
			"var data interface{}",
			"json.Unmarshal(body, &data)",
		}
		issues := checkUnsafeDeserialization("test.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected unsafe deserialization issue")
		}
		if issues[0].Severity != "high" {
			t.Errorf("severity = %s, want high", issues[0].Severity)
		}
	})

	t.Run("safe unmarshal with struct", func(t *testing.T) {
		lines := []string{
			"var user User",
			"json.Unmarshal(body, &user)",
		}
		issues := checkUnsafeDeserialization("test.go", lines)
		if len(issues) > 0 {
			t.Errorf("expected no issues for typed unmarshal, got %d", len(issues))
		}
	})
}

func TestCheckInconsistentPattern(t *testing.T) {
	t.Run("mixed naming conventions", func(t *testing.T) {
		lines := []string{
			"userName := \"alice\"",
			"user_email := \"alice@example.com\"",
			"userAge := 30",
			"user_address := \"123 Main St\"",
			"displayName := \"Alice\"",
			"phone_number := \"555-0123\"",
		}
		issues := checkInconsistentPattern("test.go", lines)
		found := false
		for _, issue := range issues {
			if strings.Contains(issue.Message, "Mixed naming conventions") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected mixed naming convention issue")
		}
	})

	t.Run("consistent naming", func(t *testing.T) {
		lines := []string{
			"userName := \"alice\"",
			"userAge := 30",
			"displayName := \"Alice\"",
			"phoneNumber := \"555-0123\"",
			"emailAddress := \"a@b.com\"",
		}
		issues := checkInconsistentPattern("test.go", lines)
		for _, issue := range issues {
			if strings.Contains(issue.Message, "Mixed naming conventions") {
				t.Error("expected no mixed naming issue for consistent camelCase")
			}
		}
	})

	// Wire names in struct tags, strings and comments are not identifiers:
	// a Go file whose JSON keys are snake_case is still consistent camelCase.
	t.Run("struct tags, strings and comments are not identifiers", func(t *testing.T) {
		lines := []string{
			"type Report struct {",
			"\tAICommitShare float64 `json:\"ai_commit_share\"`",
			"\tAILineShare   float64 `json:\"ai_line_share\"`",
			"\tTotalCommits  int     `json:\"total_commits\"`",
			"\tHumanCommits  int     `json:\"human_commits\"`",
			"}",
			"func (r Report) key() string { return \"by_tool\" }",
			"func newReport(sinceDate string) Report { return Report{} }",
			"func mergeInto(byTool map[string]int, orgName string) int { return 0 }",
			"func aiShare(aiCommits, totalCommits int) float64 { return 0 } // was ai_share",
			"func lineShare(aiLines, totalLines int) float64 { return 0 } /* not line_share */",
			"func toolName(rawName string) string { return rawName + 'x' }",
		}
		for _, issue := range checkInconsistentPattern("report.go", lines) {
			if strings.Contains(issue.Message, "Mixed naming conventions") {
				t.Errorf("wire names in tags, strings and comments counted as identifiers: %s", issue.Message)
			}
		}
	})

	t.Run("mixed identifiers outside literals are still flagged", func(t *testing.T) {
		lines := []string{
			"user_name := \"userName\"",
			"userEmail := \"user_email\"",
			"user_age := 30",
			"displayName := \"display_name\"",
			"phone_number := \"phoneNumber\"",
			"emailAddress := \"email_address\"",
		}
		found := false
		for _, issue := range checkInconsistentPattern("mixed.go", lines) {
			if strings.Contains(issue.Message, "Mixed naming conventions") {
				found = true
			}
		}
		if !found {
			t.Error("expected mixed naming issue: the identifiers, not the strings, are mixed")
		}
	})

	// A YAML or Rego document embedded in a raw string is content: its
	// space indentation says nothing about the Go around it.
	t.Run("embedded document in a raw string is not indented code", func(t *testing.T) {
		lines := []string{
			"package policy",
			"",
			"const fixture = `",
			"    rules:",
			"      - name: a",
			"      - name: b",
			"    deny:",
			"      - x",
			"      - y",
			"`",
			"",
			"func load() string {",
			"\tif fixture == \"\" {",
			"\t\treturn \"\"",
			"\t}",
			"\treturn fixture",
			"}",
		}
		for _, issue := range checkInconsistentPattern("policy.go", lines) {
			if strings.Contains(issue.Message, "Mixed indentation") {
				t.Errorf("raw string content counted as indented code: %s", issue.Message)
			}
		}
	})

	t.Run("mixed indentation in code is still flagged", func(t *testing.T) {
		lines := []string{
			"func a() {",
			"\tx := 1",
			"\ty := 2",
			"\tz := 3",
			"    p := 4",
			"    q := 5",
			"    r := 6",
			"}",
		}
		found := false
		for _, issue := range checkInconsistentPattern("mixed.go", lines) {
			if strings.Contains(issue.Message, "Mixed indentation") {
				found = true
			}
		}
		if !found {
			t.Error("expected mixed indentation issue for tab and space indented code")
		}
	})
}

func TestStripLiterals(t *testing.T) {
	t.Run("single line", func(t *testing.T) {
		cases := []struct{ in, want string }{
			{`x := "a_b c_d" // e_f`, "x :=  "},
			{"tag := `json:\"a_b\"`", "tag := "},
			{`r := 'x'`, "r := "},
			{`s := "esc \" q_r"; t := u_v`, "s := ; t := u_v"},
			{`s := 'it\'s'; t := 1`, "s := ; t := 1"},
			{`y = 1  # k_v`, "y = 1  "},
			{`n := len(arr[#idx]) + ${#x}`, "n := len(arr[#idx]) + ${#x}"},
			{`#include <a_b.h>`, ""},
			{`a := b /* c_d */ + e_f`, "a := b  + e_f"},
			{`unterminated := "a_b`, "unterminated := "},
			{`plain_code := camelCase`, "plain_code := camelCase"},
		}
		for _, c := range cases {
			var st literalState
			if got := stripLiterals(c.in, &st); got != c.want {
				t.Errorf("stripLiterals(%q) = %q, want %q", c.in, got, c.want)
			}
			if st.open() {
				t.Errorf("stripLiterals(%q) left a literal open", c.in)
			}
		}
	})

	t.Run("multi-line state", func(t *testing.T) {
		cases := []struct {
			name string
			in   []string
			want []string
		}{
			{"raw string", []string{"a := `", "  x_y", "` + b_c"}, []string{"a := ", "", " + b_c"}},
			{"block comment", []string{"/* p_q", "r_s */ t_u"}, []string{"", " t_u"}},
			{"triple quote", []string{`s = """doc a_b`, "c_d", `e_f """ + g_h`}, []string{"s = ", "", " + g_h"}},
			{"struct tags stay single-line", []string{"A int `json:\"a_b\"`", "b_c := 1"}, []string{"A int ", "b_c := 1"}},
		}
		for _, c := range cases {
			var st literalState
			for i, line := range c.in {
				if got := stripLiterals(line, &st); got != c.want[i] {
					t.Errorf("%s line %d: stripLiterals(%q) = %q, want %q", c.name, i, line, got, c.want[i])
				}
			}
			if st.open() {
				t.Errorf("%s: literal still open after the last line", c.name)
			}
		}
	})
}

func TestCheckHallucinatedAPI(t *testing.T) {
	t.Run("ioutil import flagged", func(t *testing.T) {
		lines := []string{`import "io/ioutil"`}
		issues := checkHallucinatedAPI("file.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected hallucinated API issue for io/ioutil import, got none")
		}
		if issues[0].Rule != "ai-hallucinated-api" {
			t.Errorf("rule = %s, want ai-hallucinated-api", issues[0].Rule)
		}
		if issues[0].Severity != "medium" {
			t.Errorf("severity = %s, want medium", issues[0].Severity)
		}
	})

	t.Run("ioutil.ReadAll call flagged", func(t *testing.T) {
		lines := []string{"data, err := ioutil.ReadAll(r)"}
		issues := checkHallucinatedAPI("file.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected hallucinated API issue for ioutil.ReadAll, got none")
		}
		if issues[0].Severity != "medium" {
			t.Errorf("severity = %s, want medium", issues[0].Severity)
		}
	})

	t.Run("ioutil.ReadFile call flagged", func(t *testing.T) {
		lines := []string{`data, err := ioutil.ReadFile("config.json")`}
		issues := checkHallucinatedAPI("file.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected hallucinated API issue for ioutil.ReadFile, got none")
		}
	})

	t.Run("rand.Seed flagged as info", func(t *testing.T) {
		lines := []string{"rand.Seed(time.Now().UnixNano())"}
		issues := checkHallucinatedAPI("file.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected hallucinated API issue for rand.Seed, got none")
		}
		if issues[0].Severity != "info" {
			t.Errorf("severity = %s, want info", issues[0].Severity)
		}
	})

	t.Run("modern io.ReadAll not flagged", func(t *testing.T) {
		lines := []string{
			"data, err := io.ReadAll(r)",
			`data, err := os.ReadFile("config.json")`,
		}
		issues := checkHallucinatedAPI("file.go", lines)
		if len(issues) > 0 {
			t.Errorf("expected no issues for modern API usage, got %d", len(issues))
		}
	})

	t.Run("comments not flagged", func(t *testing.T) {
		lines := []string{
			"// Use ioutil.ReadAll for reading — deprecated",
			"// rand.Seed was used before Go 1.20",
		}
		issues := checkHallucinatedAPI("file.go", lines)
		if len(issues) > 0 {
			t.Errorf("expected no issues for comment lines, got %d", len(issues))
		}
	})
}

func TestAnalyzeAllRules(t *testing.T) {
	t.Run("AI-like code triggers multiple rules", func(t *testing.T) {
		files := map[string][]string{
			"auth.go": {
				"// This function handles authentication",
				"// It validates the user credentials",
				"// Returns a token on success",
				"// The token expires after 24 hours",
				"// We store it in a secure cookie",
				"func authenticate_user(credentials interface{}) error {",
				"    var data interface{}",
				"    json.Unmarshal(nil, &data)",
				"    if err != nil {",
				"        return err",
				"    }",
				"    if err != nil {",
				"        return err",
				"    }",
				"    return nil",
				"}",
			},
		}

		result := Analyze(Options{Files: files})
		if len(result.Issues) == 0 {
			t.Error("expected multiple issues for AI-like code")
		}

		counts := result.IssueCounts()
		t.Logf("Issue counts: %v", counts)
		t.Logf("Issues: %+v", result.Issues)
	})

	t.Run("clean code produces no issues", func(t *testing.T) {
		files := map[string][]string{
			"math.go": {
				"func add(a, b int) int { return a + b }",
				"func sub(a, b int) int { return a - b }",
				"func mul(a, b int) int { return a * b }",
			},
		}

		result := Analyze(Options{Files: files})
		if len(result.Issues) > 0 {
			t.Errorf("expected no issues for clean code, got %d", len(result.Issues))
		}
	})
}

func TestAnalysisResultHelpers(t *testing.T) {
	r := &AnalysisResult{
		TotalLines: 100,
		Issues: []Issue{
			{Rule: "test", Severity: "critical", Line: 1},
			{Rule: "test", Severity: "high", Line: 2},
			{Rule: "test", Severity: "medium", Line: 3},
			{Rule: "test", Severity: "low", Line: 4},
			{Rule: "test", Severity: "info", Line: 5},
		},
	}

	if r.CriticalCount() != 2 {
		t.Errorf("CriticalCount = %d, want 2", r.CriticalCount())
	}
	if !r.HasCritical() {
		t.Error("HasCritical should be true")
	}
	if !r.HasHigh() {
		t.Error("HasHigh should be true")
	}
	if r.IssueDensity() != 50.0 {
		t.Errorf("IssueDensity = %f, want 50.0", r.IssueDensity())
	}

	counts := r.IssueCounts()
	if counts["critical"] != 1 || counts["high"] != 1 || counts["medium"] != 1 {
		t.Errorf("IssueCounts = %v, want 1 each", counts)
	}
}

func TestSummarizeIssues(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := summarizeIssues(nil)
		if s != "No quality issues detected" {
			t.Errorf("summary = %s, want 'No quality issues detected'", s)
		}
	})

	t.Run("mixed severities", func(t *testing.T) {
		issues := []Issue{
			{Severity: "critical"},
			{Severity: "high"},
			{Severity: "high"},
			{Severity: "medium"},
		}
		s := summarizeIssues(issues)
		if !strings.Contains(s, "1 critical") || !strings.Contains(s, "2 high") {
			t.Errorf("summary = %s, missing severity counts", s)
		}
	})
}
