package detector

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// approx reports whether a and b are equal up to floating-point noise.
func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// repeat returns n copies of line.
func repeat(line string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = line
	}
	return out
}

// aiLikeLines returns 20 lines that trip every heuristic at its top tier:
// comments outnumber code (11 of 20), a quarter of the lines are error checks,
// a fifth of the identifiers are long, and all indented code shares one indent.
func aiLikeLines() []string {
	var lines []string
	for i := 0; i < 11; i++ {
		lines = append(lines, fmt.Sprintf("// Step %d is documented in detail for the reader.", i))
	}
	lines = append(lines, repeat("\tif err != nil {", 5)...)
	for i := 0; i < 4; i++ {
		lines = append(lines, fmt.Sprintf("\tvalidatedRequestPayloadFromCaller%d := 1", i))
	}
	return lines
}

// aiLikeSource is aiLikeLines as file content.
func aiLikeSource() string { return strings.Join(aiLikeLines(), "\n") + "\n" }

// TestCommentRatio_Tiers guards each comment-density tier and its boundary
// (the ratio has to exceed a threshold, not merely reach it), that blank lines
// do not dilute the ratio, and that every comment syntax counts.
func TestCommentRatio_Tiers(t *testing.T) {
	// mix returns comments comment lines followed by code code lines.
	mix := func(comments, code int) []string {
		return append(repeat("// note", comments), repeat("x := 1", code)...)
	}
	cases := []struct {
		name  string
		lines []string
		want  float64
	}{
		{"no lines", nil, 0},
		{"only blank lines", []string{"", "   ", "\t"}, 0},
		{"all comments", mix(4, 0), 0.9},
		{"three quarters comments", mix(3, 1), 0.9},
		{"exactly half is not above half", mix(1, 1), 0.6},
		{"three eighths", mix(3, 5), 0.6},
		{"exactly 35 percent is not above 35 percent", mix(7, 13), 0.3},
		{"thirty percent", mix(3, 7), 0.3},
		{"exactly a quarter is not above a quarter", mix(1, 3), 0},
		{"ten percent", mix(1, 9), 0},
		{"no comments", mix(0, 5), 0},
		{"blank lines do not dilute the ratio", []string{"// a", "", "x := 1", "", ""}, 0.6},
		{"indented comment counts", []string{"    // a", "x := 1"}, 0.6},
		{"hash, block and doc-comment syntaxes", []string{"# a", "/* b", " * c", " */", "x := 1"}, 0.9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commentRatio(tc.lines); !approx(got, tc.want) {
				t.Errorf("commentRatio = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestVerboseNamingScore_Tiers guards the share of long identifiers (21 or
// more characters, starting lower case) against its tier boundaries, and that
// comment lines, exported names and lines without identifiers do not count.
func TestVerboseNamingScore_Tiers(t *testing.T) {
	// identifierLine returns one line of short short identifiers and long
	// long ones, so the long share is long/(short+long).
	identifierLine := func(short, long int) []string {
		var words []string
		for i := 0; i < short; i++ {
			words = append(words, fmt.Sprintf("v%d", i))
		}
		for i := 0; i < long; i++ {
			words = append(words, fmt.Sprintf("extremelyDescriptiveIdentifier%d", i))
		}
		return []string{strings.Join(words, " ")}
	}
	cases := []struct {
		name  string
		lines []string
		want  float64
	}{
		{"no lines", nil, 0},
		{"no identifiers", []string{"{", "}", "1 + 2"}, 0},
		{"no long identifiers", identifierLine(10, 0), 0},
		{"half the identifiers are long", identifierLine(1, 1), 0.7},
		{"just above 15 percent", identifierLine(16, 3), 0.7},
		{"exactly 15 percent is not above 15 percent", identifierLine(17, 3), 0.4},
		{"just above 8 percent", identifierLine(22, 2), 0.4},
		{"exactly 8 percent is not above 8 percent", identifierLine(23, 2), 0},
		{"five percent", identifierLine(19, 1), 0},
		{"21 characters is long", []string{"x := " + strings.Repeat("a", 21)}, 0.7},
		{"20 characters is not long", []string{"x := " + strings.Repeat("a", 20)}, 0},
		{"exported names are not variable names", []string{"x := ExtremelyDescriptiveIdentifierName"}, 0},
		{
			"comment lines are skipped",
			[]string{"// extremelyDescriptiveIdentifierName", "# extremelyDescriptiveIdentifierName", "x := 1"},
			0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := verboseNamingScore(tc.lines); !approx(got, tc.want) {
				t.Errorf("verboseNamingScore = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRedundantErrorHandlingScore_Tiers guards the share of lines that open an
// "if err != nil {" block against its tier boundaries, that blank lines are
// not counted as lines, and which conditions do and do not match.
func TestRedundantErrorHandlingScore_Tiers(t *testing.T) {
	// mix returns errs error checks (indented) followed by other plain lines.
	mix := func(errs, other int) []string {
		return append(repeat("\tif err != nil {", errs), repeat("x := 1", other)...)
	}
	cases := []struct {
		name  string
		lines []string
		want  float64
	}{
		{"no lines", nil, 0},
		{"only blank lines", []string{"", "  ", "\t"}, 0},
		{"a quarter of the lines", mix(1, 3), 0.8},
		{"exactly 20 percent is not above 20 percent", mix(2, 8), 0.5},
		{"one line in seven", mix(1, 6), 0.5},
		{"exactly 12 percent is not above 12 percent", mix(3, 22), 0.25},
		{"one line in eleven", mix(1, 10), 0.25},
		{"exactly 8 percent is not above 8 percent", mix(2, 23), 0},
		{"one line in twenty-one", mix(1, 20), 0},
		{"no error checks", mix(0, 5), 0},
		{"blank lines are not counted", append(mix(1, 3), "", "", "", "", ""), 0.8},
		{"an else-if check matches", []string{"} else if err != nil {", "a := 1", "b := 2", "c := 3"}, 0.8},
		{
			"other conditions do not match",
			[]string{"if err == nil {", "if err != nil", "if errs != nil {", "x := 1"},
			0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redundantErrorHandlingScore(tc.lines); !approx(got, tc.want) {
				t.Errorf("redundantErrorHandlingScore = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestUniformIndentScore_Tiers guards the share of indented code lines that
// use the most common indent against its tier boundaries, the three-line
// minimum, and that comments and blank lines are left out.
func TestUniformIndentScore_Tiers(t *testing.T) {
	// mix returns narrow lines indented four spaces followed by wide lines
	// indented eight.
	mix := func(narrow, wide int) []string {
		return append(repeat("    x := 1", narrow), repeat("        x := 1", wide)...)
	}
	cases := []struct {
		name  string
		lines []string
		want  float64
	}{
		{"no lines", nil, 0},
		{"nothing indented", repeat("x := 1", 5), 0},
		{"two indented lines are too few", mix(2, 0), 0},
		{"three indented lines are enough", mix(3, 0), 0.7},
		{"90 percent share one indent", mix(18, 2), 0.7},
		{"exactly 85 percent is not above 85 percent", mix(17, 3), 0.4},
		{"80 percent share one indent", mix(8, 2), 0.4},
		{"exactly 70 percent is not above 70 percent", mix(7, 3), 0},
		{"an even split", mix(5, 5), 0},
		{"tabs count one column each", repeat("\tx := 1", 3), 0.7},
		{
			// Counting the ten comments (indent 8) would leave only 3 of 13 on
			// the dominant indent and score lower.
			"comments are skipped",
			append(mix(3, 0), append(repeat("        // note", 10), repeat("  # note", 5)...)...),
			0.7,
		},
		{"blank lines are skipped", append(mix(3, 0), "", "    ", "\t"), 0.7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := uniformIndentScore(tc.lines); !approx(got, tc.want) {
				t.Errorf("uniformIndentScore = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestScoreAIPatterns_Weights guards how the four heuristics combine. Each
// corpus below trips exactly one heuristic at its top tier, so the score is
// that tier times its weight (0.3 comments, 0.25 naming, 0.25 error handling,
// 0.2 indentation); a corpus that trips all four reaches the 0.785 maximum.
func TestScoreAIPatterns_Weights(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  float64
	}{
		{"no lines", nil, 0},
		{"comment density only", repeat("// note", 4), 0.9 * 0.3},
		{"long names only", []string{"var extremelyDescriptiveIdentifierName = 1"}, 0.7 * 0.25},
		{"error checks only", repeat("if err != nil {", 5), 0.8 * 0.25},
		{"uniform indentation only", repeat("    x := 1", 4), 0.7 * 0.2},
		{"every heuristic", aiLikeLines(), 0.9*0.3 + 0.7*0.25 + 0.8*0.25 + 0.7*0.2},
		{"terse human code", []string{"func add(a, b int) int { return a + b }"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scoreAIPatterns(tc.lines); !approx(got, tc.want) {
				t.Errorf("scoreAIPatterns = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDetectionResult_aggregate_verdict guards the verdict and the summary text
// aggregate derives: the 0.3 threshold, where the baseline confidence comes
// from when only per-file results exist, and how the file counts are totaled.
func TestDetectionResult_aggregate_verdict(t *testing.T) {
	cases := []struct {
		name        string
		result      DetectionResult
		wantAI      bool
		wantConf    float64
		wantSummary string
	}{
		{
			name:        "nothing found",
			result:      DetectionResult{},
			wantAI:      false,
			wantConf:    0,
			wantSummary: "No AI code detected",
		},
		{
			name:        "a weak signal is reported with its confidence",
			result:      DetectionResult{Evidence: []Evidence{{Source: "branch-name", Confidence: 0.2}}},
			wantAI:      false,
			wantConf:    0.2,
			wantSummary: "No AI code detected (confidence: 20%)",
		},
		{
			name:        "just below the threshold",
			result:      DetectionResult{Evidence: []Evidence{{Source: "branch-name", Confidence: 0.29}}},
			wantAI:      false,
			wantConf:    0.29,
			wantSummary: "No AI code detected (confidence: 29%)",
		},
		{
			name:        "the threshold itself detects",
			result:      DetectionResult{Evidence: []Evidence{{Source: "branch-name", Confidence: 0.3}}},
			wantAI:      true,
			wantConf:    0.3,
			wantSummary: "AI code detected (confidence: 30%)",
		},
		{
			name:        "evidence without files",
			result:      DetectionResult{Evidence: []Evidence{{Source: "commit-trailer", Confidence: 0.9}}},
			wantAI:      true,
			wantConf:    0.9,
			wantSummary: "AI code detected (confidence: 90%)",
		},
		{
			name: "files alone set the baseline",
			result: DetectionResult{
				Files: []FileDetection{{Path: "a.go", AILines: 5, TotalLines: 10, Confidence: 0.6}},
			},
			wantAI:      true,
			wantConf:    0.6,
			wantSummary: "AI code detected in 1 file(s)",
		},
		{
			name: "a file more confident than the evidence raises the baseline",
			result: DetectionResult{
				Evidence: []Evidence{{Source: "diff-heuristics", Confidence: 0.4}},
				Files:    []FileDetection{{Path: "a.go", AILines: 7, TotalLines: 10, Confidence: 0.7}},
			},
			wantAI:      true,
			wantConf:    0.7,
			wantSummary: "7/10 lines",
		},
		{
			name: "line counts are totaled across files",
			result: DetectionResult{
				Files: []FileDetection{
					{Path: "a.go", AILines: 3, TotalLines: 10, Confidence: 0.5},
					{Path: "b.go", AILines: 7, TotalLines: 20, Confidence: 0.5},
				},
			},
			wantAI:      true,
			wantConf:    0.5,
			wantSummary: "10/30 lines (confidence: 50%)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.result
			r.aggregate()
			if r.AIGenerated != tc.wantAI {
				t.Errorf("AIGenerated = %t, want %t", r.AIGenerated, tc.wantAI)
			}
			if !approx(r.Confidence, tc.wantConf) {
				t.Errorf("Confidence = %v, want %v", r.Confidence, tc.wantConf)
			}
			if !strings.Contains(r.Summary, tc.wantSummary) {
				t.Errorf("Summary = %q, want it to contain %q", r.Summary, tc.wantSummary)
			}
		})
	}
}
