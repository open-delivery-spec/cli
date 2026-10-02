package analyzer

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/open-delivery-spec/cli/internal/rules"
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

// ── Test helpers ───────────────────────────────────────────────

// repeatLine returns n copies of line.
func repeatLine(line string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = line
	}
	return out
}

// issuesWithSeverities builds one Issue per severity, in the order given.
func issuesWithSeverities(severities ...string) []Issue {
	issues := make([]Issue, len(severities))
	for i, s := range severities {
		issues[i] = Issue{Rule: "test", Severity: s, Line: i + 1}
	}
	return issues
}

// resultWith builds an AnalysisResult of the given size holding one issue per
// severity.
func resultWith(totalLines int, severities ...string) *AnalysisResult {
	return &AnalysisResult{TotalLines: totalLines, Issues: issuesWithSeverities(severities...)}
}

// closeTo reports whether a and b are equal within floating-point noise.
func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// ── Analyze ────────────────────────────────────────────────────

// TestAnalyze_NoFiles guards the empty-input contract: a nil file map means
// there was nothing to analyze and says so, while an empty map or an empty file
// was analyzed and is clean. Either way Issues is a non-nil empty slice, so the
// JSON output carries "issues":[] rather than null.
func TestAnalyze_NoFiles(t *testing.T) {
	cases := []struct {
		name        string
		opts        Options
		wantSummary string
	}{
		{"nil file map", Options{}, "No code to analyze"},
		{"empty file map", Options{Files: map[string][]string{}}, "No quality issues detected"},
		{"file without lines", Options{Files: map[string][]string{"empty.go": nil}}, "No quality issues detected"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := Analyze(c.opts)
			if res.Summary != c.wantSummary {
				t.Errorf("Summary = %q, want %q", res.Summary, c.wantSummary)
			}
			if res.TotalLines != 0 || res.AILines != 0 {
				t.Errorf("TotalLines/AILines = %d/%d, want 0/0", res.TotalLines, res.AILines)
			}
			if res.Issues == nil || len(res.Issues) != 0 {
				t.Errorf("Issues = %#v, want a non-nil empty slice", res.Issues)
			}
			raw, err := json.Marshal(res)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(raw), `"issues":[]`) {
				t.Errorf("JSON = %s, want an empty issues array", raw)
			}
		})
	}
}

// TestAnalyze_MultipleFiles guards aggregation across files: line totals are
// summed, every issue keeps the file it came from, and the summary counts the
// issues of all files. Map iteration order is random, so issues are keyed by file.
func TestAnalyze_MultipleFiles(t *testing.T) {
	res := Analyze(Options{Files: map[string][]string{
		"old.go":   {`import "io/ioutil"`, "x := 1"},
		"clean.go": {"func add(a, b int) int { return a + b }"},
		"seed.go":  {"rand.Seed(1)", "y := 2", "z := 3"},
	}})

	if res.TotalLines != 6 {
		t.Errorf("TotalLines = %d, want 6 (2 + 1 + 3)", res.TotalLines)
	}
	byFile := map[string]Issue{}
	for _, iss := range res.Issues {
		if prev, dup := byFile[iss.File]; dup {
			t.Errorf("file %s reported twice: %+v and %+v", iss.File, prev, iss)
		}
		byFile[iss.File] = iss
	}
	if len(byFile) != 2 {
		t.Fatalf("issues came from %d files (%v), want old.go and seed.go only", len(byFile), res.Issues)
	}
	if iss := byFile["old.go"]; iss.Rule != rules.HallucinatedAPI || iss.Severity != "medium" || iss.Line != 1 {
		t.Errorf("old.go issue = %+v, want a medium %s on line 1", iss, rules.HallucinatedAPI)
	}
	if iss := byFile["seed.go"]; iss.Rule != rules.HallucinatedAPI || iss.Severity != "info" || iss.Line != 1 {
		t.Errorf("seed.go issue = %+v, want an info %s on line 1", iss, rules.HallucinatedAPI)
	}
	if want := "2 issue(s) found: 1 medium, 1 info"; res.Summary != want {
		t.Errorf("Summary = %q, want %q", res.Summary, want)
	}
}

// ── summarizeIssues / ResummarizeSARIF ─────────────────────────

// TestSummarizeIssues_Breakdown guards the summary wording: the leading count is
// the total number of issues, only severities that occur are named, and they
// are always listed from most to least severe whatever the input order.
func TestSummarizeIssues_Breakdown(t *testing.T) {
	cases := []struct {
		name       string
		severities []string
		want       string
	}{
		{"only critical", []string{"critical"}, "1 issue(s) found: 1 critical"},
		{"only low", []string{"low"}, "1 issue(s) found: 1 low"},
		{"only info", []string{"info"}, "1 issue(s) found: 1 info"},
		{"low sorts between medium and info", []string{"info", "low", "low", "medium"}, "4 issue(s) found: 1 medium, 2 low, 1 info"},
		{
			"every severity in scrambled order",
			[]string{"info", "low", "medium", "high", "critical", "high"},
			"6 issue(s) found: 1 critical, 2 high, 1 medium, 1 low, 1 info",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := summarizeIssues(issuesWithSeverities(c.severities...)); got != c.want {
				t.Errorf("summarizeIssues = %q, want %q", got, c.want)
			}
		})
	}
}

// TestResummarizeSARIF guards the post-merge summary: once findings ingested
// from SARIF are appended to a result, the regenerated summary reflects the
// merged list, and an empty list reads as clean.
func TestResummarizeSARIF(t *testing.T) {
	res := Analyze(Options{Files: map[string][]string{"old.go": {`import "io/ioutil"`}}})
	if want := "1 issue(s) found: 1 medium"; res.Summary != want {
		t.Fatalf("Summary before merge = %q, want %q", res.Summary, want)
	}

	res.Issues = append(res.Issues,
		Issue{Rule: "sarif/CWE-79", File: "web.js", Line: 3, Severity: "critical"},
		Issue{Rule: "sarif/style", File: "web.js", Line: 9, Severity: "low"},
	)
	if got, want := ResummarizeSARIF(res.Issues), "3 issue(s) found: 1 critical, 1 medium, 1 low"; got != want {
		t.Errorf("ResummarizeSARIF = %q, want %q", got, want)
	}
	if got := ResummarizeSARIF(nil); got != "No quality issues detected" {
		t.Errorf("ResummarizeSARIF(nil) = %q, want %q", got, "No quality issues detected")
	}
}

// ── Rule: ai-redundant-error-handling ──────────────────────────

// TestCheckRedundantErrorHandling_Proximity guards the "close proximity" window:
// two checks up to five lines apart are dense, six apart are not; the issue is
// reported on the last block and counts every block of a dense run.
func TestCheckRedundantErrorHandling_Proximity(t *testing.T) {
	const check = "if err != nil {"
	// blocksAt places an error check on each given 0-based line of a file long
	// enough to hold them, with plain statements everywhere else.
	blocksAt := func(at ...int) []string {
		lines := repeatLine("x := 1", at[len(at)-1]+1)
		for _, i := range at {
			lines[i] = check
		}
		return lines
	}

	cases := []struct {
		name     string
		lines    []string
		wantLine int // 1-based line of the last block; 0 = no issue
		wantMsg  string
	}{
		{"five lines apart is dense", blocksAt(0, 5), 6, "2 consecutive error handling blocks"},
		{"six lines apart is not", blocksAt(0, 6), 0, ""},
		{"a run of three counts all three", blocksAt(0, 3, 6), 7, "3 consecutive error handling blocks"},
		{"a single check is never dense", blocksAt(4), 0, ""},
		{"indented checks are recognized", []string{"\t" + check, "\t\t" + check}, 2, "2 consecutive error handling blocks"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := checkRedundantErrorHandling("e.go", c.lines)
			if c.wantLine == 0 {
				if len(issues) != 0 {
					t.Fatalf("got %d issue(s), want none: %+v", len(issues), issues)
				}
				return
			}
			if len(issues) != 1 {
				t.Fatalf("got %d issue(s), want 1: %+v", len(issues), issues)
			}
			iss := issues[0]
			if iss.Rule != rules.RedundantErrorHandling || iss.Severity != "info" || iss.File != "e.go" {
				t.Errorf("issue = %+v, want an info %s for e.go", iss, rules.RedundantErrorHandling)
			}
			if iss.Line != c.wantLine {
				t.Errorf("Line = %d, want %d (the last block)", iss.Line, c.wantLine)
			}
			if !strings.Contains(iss.Message, c.wantMsg) {
				t.Errorf("Message = %q, want it to contain %q", iss.Message, c.wantMsg)
			}
		})
	}
}

// ── Rule: ai-over-commenting ───────────────────────────────────

// TestCheckOverCommenting_NoCountableLines guards the empty denominator: a file
// with nothing but blank lines has no comment ratio at all, so it must yield no
// issue (not a NaN percentage).
func TestCheckOverCommenting_NoCountableLines(t *testing.T) {
	cases := map[string][]string{
		"nil":                nil,
		"empty":              {},
		"blank lines":        {"", "", ""},
		"whitespace only":    {"   ", "\t", " \t "},
		"mixed blank styles": {"", "  ", "\t\t", ""},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			if issues := checkOverCommenting("blank.go", lines); len(issues) != 0 {
				t.Errorf("got %d issue(s) for %s input, want none: %+v", len(issues), name, issues)
			}
		})
	}
}

// TestCheckOverCommenting_Ratio guards how the ratio is formed: blank lines are
// ignored rather than diluting it, every comment marker counts as a comment,
// trailing comments do not turn a code line into a comment, and the 40%
// threshold is inclusive.
func TestCheckOverCommenting_Ratio(t *testing.T) {
	cases := []struct {
		name    string
		lines   []string
		wantMsg string // "" = no issue expected
	}{
		{
			"blank lines do not dilute the ratio",
			append([]string{"// one", "// two", "x := 1", "y := 2", "z := 3"}, repeatLine("", 10)...),
			"Comment-to-code ratio is 40%",
		},
		{"exactly 40 percent is flagged", []string{"// a", "// b", "x := 1", "y := 2", "z := 3"}, "Comment-to-code ratio is 40%"},
		{"just under 40 percent is not", []string{"// a", "// b", "// c", "x := 1", "y := 2", "z := 3", "w := 4", "v := 5"}, ""},
		{"rounds to a whole percent", []string{"// a", "// b", "x := 1"}, "Comment-to-code ratio is 67%"},
		{"all comments", []string{"// a", "# b"}, "Comment-to-code ratio is 100%"},
		{"slash slash marker", []string{"// note", "x := 1"}, "Comment-to-code ratio is 50%"},
		{"hash marker", []string{"# note", "x = 1"}, "Comment-to-code ratio is 50%"},
		{"block comment opener", []string{"/* note", "x := 1"}, "Comment-to-code ratio is 50%"},
		{"block comment body", []string{" * note", "x := 1"}, "Comment-to-code ratio is 50%"},
		{"block comment closer", []string{" */", "x := 1"}, "Comment-to-code ratio is 50%"},
		{"indented comment", []string{"\t\t// note", "x := 1"}, "Comment-to-code ratio is 50%"},
		{"trailing comments are code lines", []string{"x := 1 // one", "y := 2 // two", "z := 3 /* three */"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := checkOverCommenting("doc.go", c.lines)
			if c.wantMsg == "" {
				if len(issues) != 0 {
					t.Fatalf("got %d issue(s), want none: %+v", len(issues), issues)
				}
				return
			}
			if len(issues) != 1 {
				t.Fatalf("got %d issue(s), want 1: %+v", len(issues), issues)
			}
			iss := issues[0]
			if iss.Message != c.wantMsg {
				t.Errorf("Message = %q, want %q", iss.Message, c.wantMsg)
			}
			if iss.Rule != rules.OverCommenting || iss.Severity != "info" || iss.File != "doc.go" || iss.Line != 1 {
				t.Errorf("issue = %+v, want an info %s for doc.go on line 1", iss, rules.OverCommenting)
			}
		})
	}
}

// ── Rule: ai-unsafe-deserialization ────────────────────────────

// TestCheckUnsafeDeserialization_Targets guards which Unmarshal calls are
// flagged: the target must be an interface{} variable declared with var or :=
// at most five lines earlier, addressed as &v or &(v); each call is reported
// once, on its own line, naming the variable.
func TestCheckUnsafeDeserialization_Targets(t *testing.T) {
	filler := func(n int) []string { return repeatLine("n++", n) }
	join := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	cases := []struct {
		name      string
		lines     []string
		wantLines []int  // 1-based lines of the expected issues
		wantVar   string // variable named in the first issue's message
	}{
		{
			"short declaration of a generic map",
			[]string{"payload := make(map[string]interface{})", "err := json.Unmarshal(body, &payload)"},
			[]int{2}, "payload",
		},
		{
			"parenthesized address",
			[]string{"var data interface{}", "json.Unmarshal(body, &(data))"},
			[]int{2}, "data",
		},
		{
			"unmarshal five lines after the declaration",
			join([]string{"var data interface{}"}, filler(4), []string{"json.Unmarshal(body, &data)"}),
			[]int{6}, "data",
		},
		{
			"unmarshal six lines after the declaration",
			join([]string{"var data interface{}"}, filler(5), []string{"json.Unmarshal(body, &data)"}),
			nil, "",
		},
		{
			"unmarshal before the declaration",
			[]string{"json.Unmarshal(body, &data)", "var data interface{}"},
			nil, "",
		},
		{
			"typed target next to an interface{} variable",
			[]string{"var data interface{}", "var user User", "json.Unmarshal(body, &user)"},
			nil, "",
		},
		{
			"each unmarshal call is reported on its own line",
			[]string{"var a interface{}", "var b interface{}", "json.Unmarshal(x, &a)", "json.Unmarshal(y, &b)"},
			[]int{3, 4}, "",
		},
		{
			"two interface{} variables in range still yield one issue per call",
			[]string{"var a interface{}", "var b interface{}", "json.Unmarshal(x, &a)"},
			[]int{3}, "a",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := checkUnsafeDeserialization("de.go", c.lines)
			if len(issues) != len(c.wantLines) {
				t.Fatalf("got %d issue(s), want %d: %+v", len(issues), len(c.wantLines), issues)
			}
			for i, iss := range issues {
				if iss.Line != c.wantLines[i] {
					t.Errorf("issue %d: Line = %d, want %d", i, iss.Line, c.wantLines[i])
				}
				if iss.Rule != rules.UnsafeDeserialization || iss.Severity != "high" || iss.File != "de.go" {
					t.Errorf("issue %d = %+v, want a high %s for de.go", i, iss, rules.UnsafeDeserialization)
				}
			}
			if c.wantVar != "" && !strings.Contains(issues[0].Message, "("+c.wantVar+")") {
				t.Errorf("Message = %q, want it to name variable %q", issues[0].Message, c.wantVar)
			}
		})
	}
}

// ── Rule: ai-inconsistent-pattern ──────────────────────────────

// TestCheckInconsistentPattern_NamingThresholds guards when mixed identifier
// styles are reported: both styles present, more than five identifiers in
// total, and the minority style strictly above 15% — whichever style is the
// minority.
func TestCheckInconsistentPattern_NamingThresholds(t *testing.T) {
	cases := []struct {
		name         string
		camel, snake int
		wantIssue    bool
	}{
		{"snake_case minority well above the cutoff", 5, 2, true},
		{"camelCase minority well above the cutoff", 2, 5, true},
		{"even split", 3, 3, true},
		{"minority exactly at 15 percent is tolerated", 17, 3, false},
		{"stray snake_case among camelCase", 10, 1, false},
		{"stray camelCase among snake_case", 1, 10, false},
		{"too few identifiers to judge", 4, 1, false},
		{"only camelCase", 8, 0, false},
		{"only snake_case", 0, 8, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := append(repeatLine("fooBar := 1", c.camel), repeatLine("foo_bar := 1", c.snake)...)
			issues := checkInconsistentPattern("names.go", lines)
			if !c.wantIssue {
				if len(issues) != 0 {
					t.Fatalf("got %d issue(s), want none: %+v", len(issues), issues)
				}
				return
			}
			if len(issues) != 1 {
				t.Fatalf("got %d issue(s), want 1: %+v", len(issues), issues)
			}
			iss := issues[0]
			if iss.Rule != rules.InconsistentPattern || iss.Severity != "medium" || iss.File != "names.go" || iss.Line != 1 {
				t.Errorf("issue = %+v, want a medium %s for names.go on line 1", iss, rules.InconsistentPattern)
			}
			if want := fmt.Sprintf("%d camelCase + %d snake_case", c.camel, c.snake); !strings.Contains(iss.Message, want) {
				t.Errorf("Message = %q, want it to contain %q", iss.Message, want)
			}
		})
	}
}

// TestCheckInconsistentPattern_IndentationThresholds guards when mixed
// indentation is reported: both tab- and space-indented lines present and more
// than five indented lines in total.
func TestCheckInconsistentPattern_IndentationThresholds(t *testing.T) {
	cases := []struct {
		name         string
		tabs, spaces int
		wantIssue    bool
	}{
		{"even split above the minimum", 3, 3, true},
		{"mostly spaces", 1, 5, true},
		{"five lines in total is too few", 3, 2, false},
		{"only tabs", 9, 0, false},
		{"only spaces", 0, 9, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := append(repeatLine("\tx := 1", c.tabs), repeatLine("    x := 1", c.spaces)...)
			issues := checkInconsistentPattern("indent.go", lines)
			if !c.wantIssue {
				if len(issues) != 0 {
					t.Fatalf("got %d issue(s), want none: %+v", len(issues), issues)
				}
				return
			}
			if len(issues) != 1 {
				t.Fatalf("got %d issue(s), want 1: %+v", len(issues), issues)
			}
			iss := issues[0]
			if iss.Rule != rules.InconsistentPattern || iss.Severity != "low" || iss.File != "indent.go" || iss.Line != 1 {
				t.Errorf("issue = %+v, want a low %s for indent.go on line 1", iss, rules.InconsistentPattern)
			}
			if want := fmt.Sprintf("%d tab-indented + %d space-indented", c.tabs, c.spaces); !strings.Contains(iss.Message, want) {
				t.Errorf("Message = %q, want it to contain %q", iss.Message, want)
			}
		})
	}
}

// TestCheckInconsistentPattern_LiteralTails guards the line on which a raw
// string or block comment ends with code after the closer: it starts inside the
// literal, so its leading spaces are content and must not be counted as space
// indentation. Without that rule the three tab-indented lines plus three such
// tails would read as a mixed-indentation file.
func TestCheckInconsistentPattern_LiteralTails(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
	}{
		{"raw string closed before trailing code", []string{
			"\ta := `",
			"    ` + x",
			"\tb := `",
			"    ` + y",
			"\tc := `",
			"    ` + z",
		}},
		{"block comment closed before trailing code", []string{
			"\ta := 1",
			"\tb := 2",
			"\tc := 3",
			"\t/*",
			"    */ d := 4",
			"\t/*",
			"    */ e := 5",
			"\t/*",
			"    */ f := 6",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, iss := range checkInconsistentPattern("tail.go", c.lines) {
				t.Errorf("unexpected issue: %+v", iss)
			}
		})
	}
}

// ── AnalysisResult helpers ─────────────────────────────────────

// TestIssueDensity guards the per-KLOC density: it scales with the line count
// and is 0, never NaN or infinite, when the result covers no lines.
func TestIssueDensity(t *testing.T) {
	cases := []struct {
		name string
		r    *AnalysisResult
		want float64
	}{
		{"no lines but issues", resultWith(0, "critical", "low"), 0},
		{"lines but no issues", resultWith(500), 0},
		{"two issues per thousand lines", resultWith(1000, "low", "info"), 2},
		{"every severity counts", resultWith(100, "critical", "high", "medium", "low", "info"), 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.r.IssueDensity()
			if math.IsNaN(got) || math.IsInf(got, 0) || !closeTo(got, c.want) {
				t.Errorf("IssueDensity = %v, want %v", got, c.want)
			}
		})
	}
}

// TestSeverityWeightedDensity guards the blocking-debt metric: only critical
// and high issues count, per thousand lines, and a result with no lines
// reports 0 however many issues it holds.
func TestSeverityWeightedDensity(t *testing.T) {
	cases := []struct {
		name string
		r    *AnalysisResult
		want float64
	}{
		{"no lines but critical issues", resultWith(0, "critical", "high"), 0},
		{"no issues", resultWith(100), 0},
		{"only critical and high count", resultWith(100, "critical", "high", "medium", "low", "info"), 20},
		{"medium, low and info never count", resultWith(100, "medium", "low", "info"), 0},
		{"one high issue in half a KLOC", resultWith(500, "high"), 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.r.SeverityWeightedDensity()
			if math.IsNaN(got) || math.IsInf(got, 0) || !closeTo(got, c.want) {
				t.Errorf("SeverityWeightedDensity = %v, want %v", got, c.want)
			}
		})
	}
}

// TestHasCriticalAndHasHigh guards that the two predicates look at one severity
// each: HasHigh is not satisfied by a critical issue, nor HasCritical by a high
// one, and lower severities satisfy neither.
func TestHasCriticalAndHasHigh(t *testing.T) {
	cases := []struct {
		name         string
		r            *AnalysisResult
		wantCritical bool
		wantHigh     bool
	}{
		{"no issues", resultWith(10), false, false},
		{"only critical", resultWith(10, "critical"), true, false},
		{"only high", resultWith(10, "high"), false, true},
		{"critical and high", resultWith(10, "high", "critical"), true, true},
		{"medium, low and info", resultWith(10, "medium", "low", "info"), false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.HasCritical(); got != c.wantCritical {
				t.Errorf("HasCritical = %v, want %v", got, c.wantCritical)
			}
			if got := c.r.HasHigh(); got != c.wantHigh {
				t.Errorf("HasHigh = %v, want %v", got, c.wantHigh)
			}
		})
	}
}

// TestCriticalCountAndIssueCounts guards the severity tallies: CriticalCount
// merges critical and high, and IssueCounts always reports all five levels,
// including those with no issues.
func TestCriticalCountAndIssueCounts(t *testing.T) {
	empty := resultWith(10)
	if got := empty.CriticalCount(); got != 0 {
		t.Errorf("CriticalCount on an empty result = %d, want 0", got)
	}
	wantZero := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0, "info": 0}
	counts := empty.IssueCounts()
	if len(counts) != len(wantZero) {
		t.Errorf("IssueCounts on an empty result = %v, want the five levels at zero", counts)
	}
	for level, want := range wantZero {
		if got, ok := counts[level]; !ok || got != want {
			t.Errorf("IssueCounts[%q] = %d (present: %v), want %d", level, got, ok, want)
		}
	}

	r := resultWith(10, "critical", "high", "high", "medium", "low", "low", "info")
	if got := r.CriticalCount(); got != 3 {
		t.Errorf("CriticalCount = %d, want 3 (1 critical + 2 high)", got)
	}
	want := map[string]int{"critical": 1, "high": 2, "medium": 1, "low": 2, "info": 1}
	got := r.IssueCounts()
	for level, n := range want {
		if got[level] != n {
			t.Errorf("IssueCounts[%q] = %d, want %d", level, got[level], n)
		}
	}
}
