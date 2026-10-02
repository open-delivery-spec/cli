package scorer

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/detector"
)

func TestScoreLowRisk(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: false,
			Confidence:  0,
			Files:       nil,
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues:     nil,
		},
		CoverageResult:    &CoverageInput{Coverage: 0.8, Source: "go"},
		TotalChangedLines: 100,
	})

	if result.Risk != "low" {
		t.Errorf("risk = %s, want low", result.Risk)
	}
	if result.TechnicalDebtDelta > 1.0 {
		t.Errorf("delta = %f, want <= 1.0 for low-risk", result.TechnicalDebtDelta)
	}
}

func TestScoreHighRisk(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true,
			Confidence:  0.85,
			Files: []detector.FileDetection{
				{Path: "auth.go", AILines: 60, TotalLines: 100, Confidence: 0.8},
			},
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues: []analyzer.Issue{
				{Rule: "test", Severity: "critical", Line: 1},
				{Rule: "test", Severity: "high", Line: 2},
				{Rule: "test", Severity: "medium", Line: 3},
			},
		},
		CoverageResult:    &CoverageInput{Coverage: 0.1, Source: "go"},
		TotalChangedLines: 100,
	})

	if result.Verdict != "increase" {
		t.Errorf("verdict = %s, want increase", result.Verdict)
	}
	if result.Risk != "high" && result.Risk != "critical" {
		t.Errorf("risk = %s, want high or critical", result.Risk)
	}
	if result.TechnicalDebtDelta < 3.0 {
		t.Errorf("delta = %f, want >= 3.0 for high-risk", result.TechnicalDebtDelta)
	}
}

func TestScoreNeutral(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true,
			Confidence:  0.5,
			Files: []detector.FileDetection{
				{Path: "util.go", AILines: 20, TotalLines: 100, Confidence: 0.4},
			},
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues: []analyzer.Issue{
				{Rule: "test", Severity: "low", Line: 1},
			},
		},
		CoverageResult:    &CoverageInput{Coverage: 0.5, Source: "go"},
		TotalChangedLines: 100,
	})

	// With moderate AI ratio (0.2), 1 low issue, and 50% test coverage,
	// tech debt delta should be reasonable (not extreme)
	b := result.Breakdown
	if b.AICodeRatio != 0.2 {
		t.Errorf("AI ratio = %f, want 0.2", b.AICodeRatio)
	}
	if b.TestCoverage != 0.5 {
		t.Errorf("test coverage = %f, want 0.5", b.TestCoverage)
	}
	// Verdict should be at most "increase" (not some weird value)
	if result.Verdict != "decrease" && result.Verdict != "neutral" && result.Verdict != "increase" {
		t.Errorf("verdict = %s, want one of: decrease, neutral, increase", result.Verdict)
	}
}

func TestScoreBreakdown(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true,
			Confidence:  0.9,
			Files: []detector.FileDetection{
				{Path: "a.go", AILines: 40, TotalLines: 100, Confidence: 0.8},
			},
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues: []analyzer.Issue{
				{Rule: "t1", Severity: "critical", Line: 1},
				{Rule: "t2", Severity: "high", Line: 2},
				{Rule: "t3", Severity: "high", Line: 3},
			},
		},
		CoverageResult:    &CoverageInput{Coverage: 0.2, Source: "go"},
		TotalChangedLines: 100,
	})

	b := result.Breakdown
	if b.AICodeRatio != 0.4 {
		t.Errorf("AI ratio = %f, want 0.4", b.AICodeRatio)
	}
	if b.CriticalIssues != 3 {
		t.Errorf("critical issues = %d, want 3", b.CriticalIssues)
	}
	if b.TestCoverage != 0.2 {
		t.Errorf("test coverage = %f, want 0.2", b.TestCoverage)
	}
}

// TestScore_CleanAIPRIsLowRisk locks the core of the quality-driven model: a
// fully AI-written change with no defects and good coverage must score ~0. AI
// quantity alone must never create technical debt.
func TestScore_CleanAIPRIsLowRisk(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD") // diff against HEAD → zero duplication, deterministic
	res := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true, Confidence: 1.0,
			Files: []detector.FileDetection{
				{Path: "a.go", AILines: 100, TotalLines: 100, Confidence: 1.0},
			},
		},
		AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100, Issues: nil},
		CoverageResult:    &CoverageInput{Coverage: 0.9, Source: "go"},
		TotalChangedLines: 100,
	})
	if res.Breakdown.AICodeRatio != 1.0 {
		t.Fatalf("AI ratio = %f, want 1.0", res.Breakdown.AICodeRatio)
	}
	if res.Risk != "low" {
		t.Errorf("clean 100%% AI PR should be low risk, got risk %q (delta %f)",
			res.Risk, res.TechnicalDebtDelta)
	}
}

// TestScore_NoFileDataIsNotGuessed locks the fix for the invented AI ratio:
// with no per-file attribution the scorer used to report
// changed_lines × confidence × 0.5 — "49%" for a change whose every commit
// was attested AI. No data means no ratio, marked as not measured.
func TestScore_NoFileDataIsNotGuessed(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD")
	res := Score(Options{
		DetectorResult:    &detector.DetectionResult{AIGenerated: true, Confidence: 1.0, Sources: []string{"commit-trailer"}},
		AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100},
		TotalChangedLines: 100,
	})
	if res.Breakdown.AICodeRatio != 0 {
		t.Errorf("AI ratio = %f, want 0: nothing measured means nothing reported", res.Breakdown.AICodeRatio)
	}
	if res.Breakdown.AICodeRatioSource != "unknown" {
		t.Errorf("ratio source = %q, want unknown", res.Breakdown.AICodeRatioSource)
	}
}

// TestScore_RatioSourceFollowsDetector: the ratio carries the provenance of
// the per-file counts it was computed from, strongest source first, and is
// capped at 1 so a denominator narrower than the numerator cannot exceed it.
func TestScore_RatioSourceFollowsDetector(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD")
	mk := func(sources ...string) *ScoreResult {
		return Score(Options{
			DetectorResult: &detector.DetectionResult{
				AIGenerated: true, Confidence: 0.9, Sources: sources,
				Files: []detector.FileDetection{{Path: "a.go", AILines: 150, TotalLines: 150, Confidence: 0.9}},
			},
			AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100},
			TotalChangedLines: 100,
		})
	}
	cases := []struct {
		sources []string
		want    string
	}{
		{[]string{"commit-trailer", "branch-name"}, "commit-trailer"},
		{[]string{"git-ai-notes", "commit-trailer"}, "git-ai"},
		{[]string{"branch-name", "diff-heuristics"}, "diff-heuristics"},
		{[]string{"pr-body"}, "unknown"},
	}
	for _, c := range cases {
		res := mk(c.sources...)
		if res.Breakdown.AICodeRatioSource != c.want {
			t.Errorf("sources %v: ratio source = %q, want %q", c.sources, res.Breakdown.AICodeRatioSource, c.want)
		}
		if res.Breakdown.AICodeRatio != 1 {
			t.Errorf("sources %v: ratio = %f, want capped at 1", c.sources, res.Breakdown.AICodeRatio)
		}
	}
}

// TestScore_VerdictIsDirectionRiskIsBand: verdict says which way the delta
// points, risk says how far. +0.1 used to be labelled "decrease".
func TestScore_VerdictIsDirectionRiskIsBand(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD") // zero duplication, deterministic
	mk := func(coverage float64, issues []analyzer.Issue) *ScoreResult {
		return Score(Options{
			DetectorResult:    &detector.DetectionResult{},
			AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100, Issues: issues},
			CoverageResult:    &CoverageInput{Coverage: coverage, Source: "go"},
			TotalChangedLines: 100,
		})
	}
	high := func(n int) []analyzer.Issue {
		var iss []analyzer.Issue
		for i := 0; i < n; i++ {
			iss = append(iss, analyzer.Issue{Rule: "t", Severity: "high", Line: i + 1})
		}
		return iss
	}
	cases := []struct {
		name          string
		res           *ScoreResult
		verdict, risk string
	}{
		{"fully covered, no issues → +0.0", mk(1.0, nil), "neutral", "low"},
		{"90% coverage → +0.1 is an increase, still low risk", mk(0.9, nil), "increase", "low"},
		{"one high finding + gap → moderate", mk(0.5, high(1)), "increase", "moderate"},
		{"three high findings → high", mk(1.0, high(3)), "increase", "high"},
		{"four high findings → critical", mk(1.0, high(4)), "increase", "critical"},
	}
	for _, c := range cases {
		if c.res.Verdict != c.verdict || c.res.Risk != c.risk {
			t.Errorf("%s: verdict=%q risk=%q (delta %.2f), want verdict=%q risk=%q",
				c.name, c.res.Verdict, c.res.Risk, c.res.TechnicalDebtDelta, c.verdict, c.risk)
		}
	}
}

// TestScore_AIRatioAmplifiesButDoesNotCreate verifies AI ratio is a bounded
// multiplier on real quality debt, not a standalone debt source: the same
// defect produces debt regardless of authorship, and AI only amplifies it
// (by at most 1.5x).
func TestScore_AIRatioAmplifiesButDoesNotCreate(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD") // zero duplication, deterministic
	mk := func(aiLines int) *ScoreResult {
		return Score(Options{
			DetectorResult: &detector.DetectionResult{
				AIGenerated: aiLines > 0, Confidence: 1.0,
				Files: []detector.FileDetection{
					{Path: "a.go", AILines: aiLines, TotalLines: 100, Confidence: 1.0},
				},
			},
			AnalyzerResult: &analyzer.AnalysisResult{
				TotalLines: 100,
				Issues:     []analyzer.Issue{{Rule: "t", Severity: "high", Line: 1}},
			},
			CoverageResult:    &CoverageInput{Coverage: 1.0, Source: "go"}, // no coverage-gap term
			TotalChangedLines: 100,
		})
	}
	human := mk(0) // AI ratio 0
	ai := mk(100)  // AI ratio 1.0

	if human.TechnicalDebtDelta <= 0 {
		t.Errorf("a real high-severity issue must produce debt regardless of AI, got %f",
			human.TechnicalDebtDelta)
	}
	if ai.TechnicalDebtDelta <= human.TechnicalDebtDelta {
		t.Errorf("AI ratio should amplify quality debt: ai=%f human=%f",
			ai.TechnicalDebtDelta, human.TechnicalDebtDelta)
	}
	if ai.TechnicalDebtDelta > human.TechnicalDebtDelta*1.5+1e-9 {
		t.Errorf("amplification must be bounded at 1.5x: ai=%f human=%f",
			ai.TechnicalDebtDelta, human.TechnicalDebtDelta)
	}
}

func TestFormatScore(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true,
			Confidence:  0.9,
			Files: []detector.FileDetection{
				{Path: "a.go", AILines: 30, TotalLines: 100, Confidence: 0.8},
			},
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues: []analyzer.Issue{
				{Rule: "t1", Severity: "critical", Line: 1},
			},
		},
		CoverageResult:    &CoverageInput{Coverage: 0.3, Source: "go"},
		TotalChangedLines: 100,
	})

	formatted := result.FormatScore()
	if !strings.Contains(formatted, "Tech Debt Delta") {
		t.Errorf("FormatScore missing header: %s", formatted)
	}
}

func TestEstimateDuplication(t *testing.T) {
	rate := estimateDuplication("")
	// Should return a number between 0 and 1 (or 0 if no git repo)
	if rate < 0 || rate > 1 {
		t.Errorf("duplication rate = %f, want 0.0-1.0", rate)
	}
}

// gitIn runs a git command in dir with a fixed identity and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestEstimateDuplication_codeFilesOnly: repeated lines in a README are not
// copy-pasted code. A docs-only change estimates 0; the same repetition in a
// code file counts.
func TestEstimateDuplication_codeFilesOnly(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "chore: init")
	t.Chdir(dir)
	t.Setenv("ODS_DIFF_BASE", "HEAD")

	repeated := strings.Repeat("| a table row that repeats itself | yes |\n", 4)
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(repeated), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	if rate := estimateDuplication("HEAD"); rate != 0 {
		t.Errorf("docs-only change: duplication = %f, want 0", rate)
	}

	code := "package p\n\n" + strings.Repeat("\tcallSomething(withArgument)\n", 4)
	if err := os.WriteFile(filepath.Join(dir, "dup.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	if rate := estimateDuplication("HEAD"); rate <= 0 {
		t.Errorf("repeated code lines: duplication = %f, want > 0", rate)
	}
}

func TestIsStructuralLine(t *testing.T) {
	structural := []string{
		"", "   ", "// a comment", "# py comment", "/* block */", "* doc",
		"}", "})", "},", ")", "]", "{", "});", "return nil", "break",
		"continue", "default:", "x := 1",
	}
	for _, s := range structural {
		if !isStructuralLine(strings.TrimSpace(s)) {
			t.Errorf("isStructuralLine(%q) = false, want true", s)
		}
	}

	meaningful := []string{
		"total += item.Price * item.Quantity",
		"result, err := process(ctx, data)",
		"if err != nil { return fmt.Errorf(\"x: %w\", err) }",
	}
	for _, s := range meaningful {
		if isStructuralLine(strings.TrimSpace(s)) {
			t.Errorf("isStructuralLine(%q) = true, want false", s)
		}
	}
}

func TestDuplicationRate(t *testing.T) {
	t.Run("structural repetition does not count", func(t *testing.T) {
		// All closing braces / trivial returns: previously inflated to a high
		// rate; must now be 0 because none are meaningful lines.
		lines := []string{
			"func a() error {", "}", "func b() error {", "}",
			"return nil", "return nil", "}", "}",
		}
		if r := duplicationRate(lines); r != 0 {
			t.Errorf("duplicationRate = %f, want 0 (structural lines excluded)", r)
		}
	})

	t.Run("real duplicated lines count", func(t *testing.T) {
		dup := "user.Roles = append(user.Roles, adminRole)"
		lines := []string{dup, "x := compute(alpha, beta)", dup, dup}
		// 4 meaningful lines counted, dup appears 3x → 2 duplicates / 4 total.
		got := duplicationRate(lines)
		want := 2.0 / 4.0
		if got < want-0.001 || got > want+0.001 {
			t.Errorf("duplicationRate = %f, want ~%f", got, want)
		}
	})

	t.Run("no added lines", func(t *testing.T) {
		if r := duplicationRate(nil); r != 0 {
			t.Errorf("duplicationRate(nil) = %f, want 0", r)
		}
	})
}

// TestScore_DeltaIndependentOfDiffSize locks the fix for the density
// explosion: one high finding must cost the same debt in a tiny diff as in a
// large one. Density is a per-KLOC ratio — charging it into the delta made
// the same finding score 100x higher on a 20-line change than on a 2000-line
// change, blocking small PRs hardest (the opposite of the real risk) and
// double-charging findings already counted absolutely.
func TestScore_DeltaIndependentOfDiffSize(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD") // zero duplication, deterministic
	mk := func(lines int) *ScoreResult {
		return Score(Options{
			AnalyzerResult: &analyzer.AnalysisResult{
				TotalLines: lines,
				Issues:     []analyzer.Issue{{Rule: "t", Severity: "high", Line: 1}},
			},
			CoverageResult:    &CoverageInput{Coverage: 1.0, Source: "go"}, // no coverage-gap term
			TotalChangedLines: lines,
		})
	}
	small, large := mk(20), mk(2000)
	if math.Abs(small.TechnicalDebtDelta-large.TechnicalDebtDelta) > 1e-9 {
		t.Errorf("delta depends on diff size: 20 lines → %f, 2000 lines → %f",
			small.TechnicalDebtDelta, large.TechnicalDebtDelta)
	}
	// The ratio itself stays visible in the breakdown for humans.
	if small.Breakdown.DefectDensity <= large.Breakdown.DefectDensity {
		t.Errorf("density should still reflect the per-KLOC ratio: small=%f large=%f",
			small.Breakdown.DefectDensity, large.Breakdown.DefectDensity)
	}
}

// TestScore_CoverageIsNeverEstimated: without a parsed coverage report the
// coverage is "not measured" (-1) and the coverage-gap term is skipped. It used
// to be estimated as test-file lines over changed lines, a number that is not
// coverage and could exceed 100%.
func TestScore_CoverageIsNeverEstimated(t *testing.T) {
	res := Score(Options{
		DetectorResult:    &detector.DetectionResult{},
		AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100},
		TotalChangedLines: 100,
		DiffBase:          "HEAD",
	})
	if res.Breakdown.TestCoverage != -1 || res.Breakdown.TestCoverageSource != "unknown" {
		t.Errorf("coverage = %f (%s), want -1 (unknown)", res.Breakdown.TestCoverage, res.Breakdown.TestCoverageSource)
	}
	if res.TechnicalDebtDelta != 0 {
		t.Errorf("delta = %f, want 0 with nothing measured", res.TechnicalDebtDelta)
	}
}

// ── Fixture helpers ────────────────────────────────────────────

// writeFixture writes a file into a fixture repository.
func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// initFixtureRepo creates a git repository holding one committed README, makes
// it the working directory for the rest of the test and returns its path. The
// duplication estimate runs git in the working directory, so tests that reach
// it use this to stay independent of the checkout the suite runs in.
func initFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	writeFixture(t, dir, "README.md", "# fixture\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "chore: init")
	t.Chdir(dir)
	return dir
}

// duplicatedCode is a Go file whose body repeats one meaningful line n times,
// so the duplication estimate for it is (n-1)/n: 0.75 for n = 4.
func duplicatedCode(n int) string {
	return "package p\n\n" + strings.Repeat("\tcallSomething(withArgument)\n", n)
}

// ── estimateDuplication ────────────────────────────────────────

// TestEstimateDuplication_FailsOpen guards the estimate's failure mode: it is
// advisory, so whenever git cannot list or produce the diff (unknown base,
// not a repository, a diff that cannot be rendered) it reports no duplication
// instead of failing the score.
func TestEstimateDuplication_FailsOpen(t *testing.T) {
	dir := initFixtureRepo(t)
	writeFixture(t, dir, "dup.go", duplicatedCode(4))
	gitIn(t, dir, "add", ".")

	t.Run("unknown base ref", func(t *testing.T) {
		if rate := estimateDuplication("no-such-ref"); rate != 0 {
			t.Errorf("duplication = %f, want 0 for an unknown base", rate)
		}
	})

	t.Run("outside a repository", func(t *testing.T) {
		outside, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Chdir(outside)
		// Stop git from discovering a repository above the temp dir.
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(outside))
		if rate := estimateDuplication("HEAD"); rate != 0 {
			t.Errorf("duplication = %f, want 0 outside a repository", rate)
		}
	})

	// Last, because it changes the fixture's configuration.
	t.Run("diff cannot be rendered", func(t *testing.T) {
		if rate := estimateDuplication("HEAD"); rate <= 0 {
			t.Fatalf("control: duplication = %f before the diff driver is broken, want > 0", rate)
		}

		// A diff driver that cannot run makes `git diff` fail while
		// `git diff --name-only` (which never invokes it) still lists the file.
		gitIn(t, dir, "config", "diff.broken.command", "ods-no-such-diff-tool")
		writeFixture(t, dir, ".gitattributes", "*.go diff=broken\n")
		gitIn(t, dir, "add", ".")
		if rate := estimateDuplication("HEAD"); rate != 0 {
			t.Errorf("duplication = %f, want 0 when the diff itself fails", rate)
		}
	})
}

// TestEstimateDuplication_BaseSelection guards which base the diff is taken
// against: an explicit argument beats ODS_DIFF_BASE, which beats the HEAD~1
// default. The fixture's only change is committed, so it is visible against
// HEAD~1 and invisible against HEAD.
func TestEstimateDuplication_BaseSelection(t *testing.T) {
	dir := initFixtureRepo(t)
	writeFixture(t, dir, "dup.go", duplicatedCode(4))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: add duplicated code")

	cases := []struct {
		name     string
		env, arg string
		want     float64
	}{
		{"nothing set defaults to HEAD~1", "", "", 0.75},
		{"the environment variable beats the default", "HEAD", "", 0},
		{"an explicit base beats the default", "", "HEAD", 0},
		{"an explicit base beats the environment variable", "HEAD", "HEAD~1", 0.75},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("ODS_DIFF_BASE", c.env)
			if rate := estimateDuplication(c.arg); rate != c.want {
				t.Errorf("duplication = %f, want %f", rate, c.want)
			}
		})
	}
}

// TestEstimateDuplication_RemovalsAreNotDuplication guards that only added
// lines are measured: shrinking a file that repeated itself leaves nothing
// added, so the estimate is 0.
func TestEstimateDuplication_RemovalsAreNotDuplication(t *testing.T) {
	dir := initFixtureRepo(t)
	writeFixture(t, dir, "dup.go", duplicatedCode(4))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: add duplicated code")

	writeFixture(t, dir, "dup.go", duplicatedCode(1))
	gitIn(t, dir, "add", ".")
	if rate := estimateDuplication("HEAD"); rate != 0 {
		t.Errorf("duplication = %f, want 0 when the change only removes repeated lines", rate)
	}
}

// TestEstimateDuplication_FileHeadersAreNotCode guards the diff parsing across
// several files: the "+++ b/<path>" header of each added file and the
// "+++ /dev/null" header of a deleted one are not code, so with long paths they
// would otherwise inflate the denominator. Two new files that each add the same
// two lines are exactly 3 duplicates out of 4.
func TestEstimateDuplication_FileHeadersAreNotCode(t *testing.T) {
	dir := initFixtureRepo(t)
	writeFixture(t, dir, "retired_module_file.go", "package p\n\nfunc retired() { doSomethingUseful() }\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: add a file to retire")

	body := duplicatedCode(2)
	writeFixture(t, dir, "internal_alpha_module.go", body)
	writeFixture(t, dir, "internal_beta_module.go", body)
	gitIn(t, dir, "rm", "-q", "retired_module_file.go")
	gitIn(t, dir, "add", ".")

	if rate := estimateDuplication("HEAD"); rate != 0.75 {
		t.Errorf("duplication = %f, want 0.75 (3 duplicates of 4 added code lines)", rate)
	}
}

// ── Score ──────────────────────────────────────────────────────

// TestScore_DuplicationFlowsIntoDelta guards the wiring from the diff to the
// score: the estimate for Options.DiffBase lands in the breakdown, adds to the
// delta one for one, and is amplified by the AI ratio like any other quality
// debt.
func TestScore_DuplicationFlowsIntoDelta(t *testing.T) {
	dir := initFixtureRepo(t)
	writeFixture(t, dir, "dup.go", duplicatedCode(4))
	gitIn(t, dir, "add", ".")

	mk := func(aiLines int) *ScoreResult {
		return Score(Options{
			DetectorResult: &detector.DetectionResult{Files: []detector.FileDetection{
				{Path: "dup.go", AILines: aiLines, TotalLines: 100, Confidence: 1},
			}},
			AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100},
			CoverageResult:    &CoverageInput{Coverage: 1.0, Source: "go"}, // no coverage-gap term
			TotalChangedLines: 100,
			DiffBase:          "HEAD",
		})
	}

	human := mk(0)
	if human.Breakdown.DuplicationRate != 0.75 || human.TechnicalDebtDelta != 0.75 {
		t.Errorf("duplication/delta = %f/%f, want 0.75/0.75", human.Breakdown.DuplicationRate, human.TechnicalDebtDelta)
	}
	if human.Verdict != "increase" || human.Risk != "low" {
		t.Errorf("verdict/risk = %s/%s, want increase/low", human.Verdict, human.Risk)
	}

	ai := mk(100)
	if ai.TechnicalDebtDelta != 0.75*1.5 {
		t.Errorf("fully AI delta = %f, want %f (amplified by 1.5)", ai.TechnicalDebtDelta, 0.75*1.5)
	}
	if ai.Risk != "moderate" {
		t.Errorf("fully AI risk = %s, want moderate", ai.Risk)
	}
}

// TestScore_NoChangedLinesSkipsRatioAndDuplication guards the zero-size change:
// with no changed lines there is no denominator for the AI ratio and nothing to
// estimate duplication on, even when the repository holds duplicated additions.
func TestScore_NoChangedLinesSkipsRatioAndDuplication(t *testing.T) {
	dir := initFixtureRepo(t)
	writeFixture(t, dir, "dup.go", duplicatedCode(4))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: add duplicated code")

	opts := Options{
		DetectorResult: &detector.DetectionResult{
			Sources: []string{"commit-trailer"},
			Files:   []detector.FileDetection{{Path: "dup.go", AILines: 50, TotalLines: 100, Confidence: 1}},
		},
		DiffBase: "HEAD~1",
	}

	opts.TotalChangedLines = 100
	if control := Score(opts); control.Breakdown.DuplicationRate != 0.75 || control.Breakdown.AICodeRatio != 0.5 {
		t.Fatalf("control: duplication/ratio = %f/%f, want 0.75/0.5", control.Breakdown.DuplicationRate, control.Breakdown.AICodeRatio)
	}

	opts.TotalChangedLines = 0
	b := Score(opts).Breakdown
	if b.DuplicationRate != 0 || b.AICodeRatio != 0 || b.AICodeRatioSource != "unknown" {
		t.Errorf("breakdown = %+v, want no duplication, no ratio and source unknown", b)
	}
}

// TestScore_CoverageSource guards the coverage provenance: a named source is
// kept, a coverage result without one is attributed to "unknown", and an absent
// result is not measured (-1) with source "unknown".
func TestScore_CoverageSource(t *testing.T) {
	cases := []struct {
		name       string
		cov        *CoverageInput
		wantCov    float64
		wantSource string
	}{
		{"named source is kept", &CoverageInput{Coverage: 0.8, Source: "lcov"}, 0.8, "lcov"},
		{"missing source becomes unknown", &CoverageInput{Coverage: 0.8}, 0.8, "unknown"},
		{"unmeasured coverage keeps the sentinel", &CoverageInput{Coverage: -1}, -1, "unknown"},
		{"no coverage result", nil, -1, "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := Score(Options{CoverageResult: c.cov})
			if res.Breakdown.TestCoverage != c.wantCov || res.Breakdown.TestCoverageSource != c.wantSource {
				t.Errorf("coverage = %f (%s), want %f (%s)",
					res.Breakdown.TestCoverage, res.Breakdown.TestCoverageSource, c.wantCov, c.wantSource)
			}
		})
	}
}

// TestScore_VerdictFollowsRoundedDelta guards the verdict at the edges of the
// one-decimal rounding: a delta that rounds to 0.0 is "neutral" on either side
// of zero, one that rounds above zero is an "increase", and one that rounds
// below zero is a "decrease". A negative delta needs a coverage figure above 1
// (a surplus against the full-coverage baseline), which no real report should
// produce, so that is the only way to reach the label.
func TestScore_VerdictFollowsRoundedDelta(t *testing.T) {
	cases := []struct {
		name        string
		coverage    float64
		wantVerdict string
	}{
		{"fully covered", 1.0, "neutral"},
		{"gap of 0.04 rounds to 0.0", 0.96, "neutral"},
		{"gap of 0.06 rounds to 0.1", 0.94, "increase"},
		{"surplus of 0.04 rounds to 0.0", 1.04, "neutral"},
		{"surplus of 0.06 rounds to -0.1", 1.06, "decrease"},
		{"coverage above 100 percent", 1.2, "decrease"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// No changed lines, so no duplication estimate: coverage is the only term.
			res := Score(Options{CoverageResult: &CoverageInput{Coverage: c.coverage, Source: "go"}})
			if res.Verdict != c.wantVerdict {
				t.Errorf("verdict = %s, want %s (delta %f)", res.Verdict, c.wantVerdict, res.TechnicalDebtDelta)
			}
			if res.Risk != "low" {
				t.Errorf("risk = %s, want low: a delta of at most 1.0 is acceptable whichever way it points", res.Risk)
			}
		})
	}
}

// TestScore_RiskBandBoundaries guards where each risk band ends: a delta of
// exactly 1.0, 3.0 or 5.0 still belongs to the lower band, and anything above
// moves up. Deltas are built from whole findings (1.5 each) and the coverage gap.
func TestScore_RiskBandBoundaries(t *testing.T) {
	cases := []struct {
		name         string
		highFindings int
		coverage     float64
		wantDelta    float64
		wantRisk     string
		wantAdvice   string
	}{
		{"exactly 1.0 is low", 0, 0.0, 1.0, "low", "Acceptable"},
		{"one finding is moderate", 1, 1.0, 1.5, "moderate", "Review recommended"},
		{"exactly 3.0 is still moderate", 2, 1.0, 3.0, "moderate", "Review recommended"},
		{"above 3.0 is high", 3, 1.0, 4.5, "high", "Add tests"},
		{"exactly 5.0 is still high", 3, 0.5, 5.0, "high", "Add tests"},
		{"above 5.0 is critical", 3, 0.0, 5.5, "critical", "Fix high/critical"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var issues []analyzer.Issue
			for i := 0; i < c.highFindings; i++ {
				issues = append(issues, analyzer.Issue{Rule: "t", Severity: "high", Line: i + 1})
			}
			// No changed lines, so no duplication estimate: findings and coverage only.
			res := Score(Options{
				AnalyzerResult: &analyzer.AnalysisResult{Issues: issues},
				CoverageResult: &CoverageInput{Coverage: c.coverage, Source: "go"},
			})
			if res.TechnicalDebtDelta != c.wantDelta {
				t.Fatalf("delta = %f, want %f", res.TechnicalDebtDelta, c.wantDelta)
			}
			if res.Risk != c.wantRisk {
				t.Errorf("risk = %s, want %s", res.Risk, c.wantRisk)
			}
			if !strings.Contains(res.Recommendation, c.wantAdvice) {
				t.Errorf("recommendation = %q, want it to contain %q", res.Recommendation, c.wantAdvice)
			}
		})
	}
}

// TestScore_FindingsWithoutLinesStillCount guards findings ingested from SARIF
// that carry no local lines: with a zero line count density is not computed,
// but every critical/high finding is still counted and charged at 1.5 each. A
// delta of exactly 3.0 is still in the "moderate" band.
func TestScore_FindingsWithoutLinesStillCount(t *testing.T) {
	res := Score(Options{
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 0,
			Issues: []analyzer.Issue{
				{Rule: "sarif/a", Severity: "high"},
				{Rule: "sarif/b", Severity: "critical"},
				{Rule: "sarif/c", Severity: "low"},
			},
		},
		CoverageResult: &CoverageInput{Coverage: 1.0, Source: "sarif"},
	})
	b := res.Breakdown
	if b.CriticalIssues != 2 {
		t.Errorf("critical issues = %d, want 2 (the low finding is not counted)", b.CriticalIssues)
	}
	if b.DefectDensity != 0 {
		t.Errorf("defect density = %f, want 0 with no lines", b.DefectDensity)
	}
	if res.TechnicalDebtDelta != 3.0 || res.Risk != "moderate" {
		t.Errorf("delta/risk = %f/%s, want 3.0/moderate", res.TechnicalDebtDelta, res.Risk)
	}
}

// TestScore_FilesAnalyzed guards the file count: the detector's file list when
// there is one, otherwise 1 when the analyzer ran (something was analyzed),
// otherwise 0.
func TestScore_FilesAnalyzed(t *testing.T) {
	two := &detector.DetectionResult{Files: []detector.FileDetection{{Path: "a.go"}, {Path: "b.go"}}}
	none := &detector.DetectionResult{}
	ran := &analyzer.AnalysisResult{}

	cases := []struct {
		name string
		opts Options
		want int
	}{
		{"detector files win", Options{DetectorResult: two, AnalyzerResult: ran}, 2},
		{"detector without files, analyzer ran", Options{DetectorResult: none, AnalyzerResult: ran}, 1},
		{"analyzer only", Options{AnalyzerResult: ran}, 1},
		{"detector without files, no analyzer", Options{DetectorResult: none}, 0},
		{"nothing ran", Options{}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Score(c.opts).FilesAnalyzed; got != c.want {
				t.Errorf("FilesAnalyzed = %d, want %d", got, c.want)
			}
		})
	}
}

// ── FormatScore ────────────────────────────────────────────────

// TestFormatScore_Output guards the one-line summary: the delta is signed, rates
// are shown as whole percentages, and coverage reads N/A only when it was not
// measured (a measured 0% is still 0%).
func TestFormatScore_Output(t *testing.T) {
	cases := []struct {
		name string
		r    ScoreResult
		want string
	}{
		{
			"everything measured",
			ScoreResult{TechnicalDebtDelta: 2.5, Risk: "moderate", Breakdown: ScoreBreakdown{
				AICodeRatio: 0.4, DefectDensity: 12.5, CriticalIssues: 3, TestCoverage: 0.3, DuplicationRate: 0.25,
			}},
			"Tech Debt Delta: +2.5 | Risk: moderate | AI Ratio: 40% | Defects: 12.5/KLOC | Critical: 3 | Coverage: 30% | Duplication: 25%",
		},
		{
			"coverage not measured",
			ScoreResult{Risk: "low", Breakdown: ScoreBreakdown{TestCoverage: -1}},
			"Tech Debt Delta: +0.0 | Risk: low | AI Ratio: 0% | Defects: 0.0/KLOC | Critical: 0 | Coverage: N/A | Duplication: 0%",
		},
		{
			"zero coverage is measured",
			ScoreResult{Risk: "low", Breakdown: ScoreBreakdown{TestCoverage: 0}},
			"Tech Debt Delta: +0.0 | Risk: low | AI Ratio: 0% | Defects: 0.0/KLOC | Critical: 0 | Coverage: 0% | Duplication: 0%",
		},
		{
			"negative delta keeps its sign",
			ScoreResult{TechnicalDebtDelta: -0.5, Risk: "low", Breakdown: ScoreBreakdown{TestCoverage: 1}},
			"Tech Debt Delta: -0.5 | Risk: low | AI Ratio: 0% | Defects: 0.0/KLOC | Critical: 0 | Coverage: 100% | Duplication: 0%",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.FormatScore(); got != c.want {
				t.Errorf("FormatScore =\n  %q\nwant\n  %q", got, c.want)
			}
		})
	}
}
