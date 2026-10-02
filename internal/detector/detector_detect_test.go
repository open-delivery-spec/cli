package detector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// aiNote renders a Git AI Standard v3 authorship log for the given
// attestation section. Its metadata declares two sessions: s_1, a Cursor
// session running claude-sonnet-4-5, and s_2, a Claude session with no model.
func aiNote(attestations string) string {
	return attestations + "\n---\n" + `{"schema_version":"authorship/3.0.0","sessions":{` +
		`"s_1":{"agent_id":{"tool":"cursor","model":"claude-sonnet-4-5"}},` +
		`"s_2":{"agent_id":{"tool":"claude"}}}}`
}

// countEvidence returns the number of evidence rows from source.
func countEvidence(res *DetectionResult, source string) int {
	n := 0
	for _, ev := range res.Evidence {
		if ev.Source == source {
			n++
		}
	}
	return n
}

// TestDetect_MaxCommitsDefault guards the default scan window: with no
// MaxCommits set, Detect looks at exactly the 10 newest commits, not at none,
// fewer or the whole history. The history has an AI commit just outside that
// window (11th newest) and another on its edge (10th newest), so only the
// second may be reported.
func TestDetect_MaxCommitsDefault(t *testing.T) {
	specs := []commitSpec{
		{msg: "feat: outside\n\n" + claudeTrailer},
		{msg: "feat: edge\n\n" + claudeTrailer},
	}
	for i := 1; i <= 9; i++ {
		specs = append(specs, commitSpec{msg: fmt.Sprintf("chore: human change %d", i)})
	}
	_, c := fixtureRepo(t, specs...) // c[1] is outside the window, c[2] on its edge

	res, err := Detect(Options{DiffBase: c[0]})
	if err != nil {
		t.Fatal(err)
	}

	if got := countEvidence(res, "commit-trailer"); got != 1 {
		t.Fatalf("attributed commits = %d, want only the one on the edge of the window", got)
	}
	short, _ := splitCommitValue(res.Evidence[0].Value)
	if !strings.HasPrefix(c[2], short) {
		t.Errorf("attributed commit %s is not the edge commit %s", short, c[2])
	}
}

// TestDetect_DefaultDiffBase guards the default base: with no DiffBase set,
// Detect reviews HEAD~1..HEAD, so an AI commit before the last one is not
// attributed to the change.
func TestDetect_DefaultDiffBase(t *testing.T) {
	fixtureRepo(t,
		commitSpec{msg: "feat: ai work\n\n" + claudeTrailer, files: map[string]string{"ai.go": "x := 1\n"}},
		commitSpec{msg: "feat: human work", files: map[string]string{"human.go": plainGo}},
	)

	res, err := Detect(Options{})
	if err != nil {
		t.Fatal(err)
	}

	if res.AIGenerated || len(res.Sources) != 0 || len(res.Evidence) != 0 {
		t.Errorf("result = %+v, want nothing detected in the human commit", res)
	}
	if res.Summary != "No AI code detected" {
		t.Errorf("Summary = %q, want %q", res.Summary, "No AI code detected")
	}
}

// TestDetect_GitAINotesOutrankTrailers guards the source precedence for
// per-file counts: when git-ai measured authorship exists, its line counts
// replace the lines the trailer commit added, the diff heuristics stay off,
// and both sources are reported, with the confidence at its cap.
func TestDetect_GitAINotesOutrankTrailers(t *testing.T) {
	_, c := fixtureRepo(t, commitSpec{
		msg:   "feat: svc\n\n" + claudeTrailer,
		files: map[string]string{"svc.go": strings.Repeat("x := 1\n", 10)},
		note:  aiNote("svc.go\n  s_1::t_1 3-5"),
	})

	res, err := Detect(Options{DiffBase: c[0]})
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"commit-trailer", "git-ai-notes"}; !slices.Equal(res.Sources, want) {
		t.Errorf("Sources = %v, want %v", res.Sources, want)
	}
	// Measured 3 lines, not the 10 the trailer commit added.
	wantFiles := []FileDetection{{Path: "svc.go", AILines: 3, TotalLines: 10, Confidence: 0.95}}
	if !reflect.DeepEqual(res.Files, wantFiles) {
		t.Errorf("Files = %+v, want %+v", res.Files, wantFiles)
	}
	if len(res.Evidence) != 2 {
		t.Fatalf("Evidence = %+v, want the trailer row then the git-ai row", res.Evidence)
	}
	short, rest := splitCommitValue(res.Evidence[1].Value)
	if !strings.HasPrefix(c[1], short) || rest != " (git-ai: 3 AI line(s), cursor/claude-sonnet-4-5)" {
		t.Errorf("git-ai evidence = %q, want the measured 3 lines and the agent for commit %s", res.Evidence[1].Value, c[1])
	}
	if res.Evidence[1].Source != "git-ai-notes" || res.Evidence[1].Signal != "authorship-log" || res.Evidence[1].Confidence != 0.95 {
		t.Errorf("git-ai evidence = %+v, want source git-ai-notes, signal authorship-log, confidence 0.95", res.Evidence[1])
	}
	if res.Confidence != MaxConfidence {
		t.Errorf("Confidence = %v, want the cap %v: two agreeing sources", res.Confidence, MaxConfidence)
	}
	if !strings.Contains(res.Summary, "3/10 lines") {
		t.Errorf("Summary = %q, want it to report the measured 3/10 lines", res.Summary)
	}
}

// TestDetect_GitAINotes guards how git-ai notes surface when nothing else
// attests the change: the evidence row names the lines and the agents, sorted
// and without duplicates (none for keys that carry no session), and the
// per-file count is capped at what the change adds.
func TestDetect_GitAINotes(t *testing.T) {
	ten := strings.Repeat("x := 1\n", 10)
	cases := []struct {
		name      string
		attest    string
		wantValue string // evidence text following "AI-assisted commit <hash>"
		wantFile  FileDetection
	}{
		{
			// The note claims 50 lines but the change adds 10: the evidence
			// reports the claim, the per-file count is capped.
			"one session, with more lines claimed than the change adds",
			"f1.go\n  s_1::t_1 1-50",
			" (git-ai: 50 AI line(s), cursor/claude-sonnet-4-5)",
			FileDetection{Path: "f1.go", AILines: 10, TotalLines: 10, Confidence: 0.95},
		},
		{
			"two sessions list their agents sorted",
			"f2.go\n  s_1::t_1 1-2\n  s_2::t_1 5",
			" (git-ai: 3 AI line(s), claude, cursor/claude-sonnet-4-5)",
			FileDetection{Path: "f2.go", AILines: 3, TotalLines: 10, Confidence: 0.95},
		},
		{
			"a legacy key has no agent to list",
			"f3.go\n  0123456789abcdef 1-4",
			" (git-ai: 4 AI line(s))",
			FileDetection{Path: "f3.go", AILines: 4, TotalLines: 10, Confidence: 0.95},
		},
	}
	specs := make([]commitSpec, len(cases))
	for i, tc := range cases {
		specs[i] = commitSpec{
			msg:   "feat: " + tc.wantFile.Path,
			files: map[string]string{tc.wantFile.Path: ten},
			note:  aiNote(tc.attest),
		}
	}
	dir, c := fixtureRepo(t, specs...)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Review just this case's commit: put HEAD on it and start the
			// range at its parent.
			gitIn(t, dir, "reset", "-q", "--hard", c[i+1])
			res, err := Detect(Options{DiffBase: c[i]})
			if err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(res.Sources, []string{"git-ai-notes"}) {
				t.Errorf("Sources = %v, want only git-ai-notes", res.Sources)
			}
			if len(res.Evidence) != 1 {
				t.Fatalf("Evidence = %+v, want one git-ai row", res.Evidence)
			}
			short, rest := splitCommitValue(res.Evidence[0].Value)
			if !strings.HasPrefix(c[i+1], short) || rest != tc.wantValue {
				t.Errorf("Value = %q, want commit %s followed by %q", res.Evidence[0].Value, c[i+1], tc.wantValue)
			}
			if len(res.Files) != 1 || res.Files[0] != tc.wantFile {
				t.Errorf("Files = %+v, want [%+v]", res.Files, tc.wantFile)
			}
			if !res.AIGenerated || res.Confidence != 0.95 {
				t.Errorf("AIGenerated = %t, Confidence = %v, want true at 0.95", res.AIGenerated, res.Confidence)
			}
		})
	}
}

// TestDetect_BranchAndPRBody guards the two signals that need no history: the
// branch name and the PR description each count on their own, a branch and a
// disclosure that agree corroborate each other (the stronger confidence plus
// one boost), and neither is reported when absent. Sources keep the
// branch-then-body order.
func TestDetect_BranchAndPRBody(t *testing.T) {
	fixtureRepo(t) // no AI commits, and no HEAD~1 for the default base to resolve
	cases := []struct {
		name        string
		branch      string
		prBody      string
		wantSources []string
		wantConf    float64
	}{
		{"neither signal", "feature/add-login", "## Summary\nplain change", nil, 0},
		{"branch only", "claude/fix-login", "", []string{"branch-name"}, 0.6},
		{"disclosure only", "", "- [x] AI-generated\n", []string{"pr-body"}, 0.85},
		{"both corroborate", "claude/fix-login", "- [x] AI-generated\n", []string{"branch-name", "pr-body"}, 0.9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Detect(Options{BranchName: tc.branch, PRBody: tc.prBody})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(res.Sources, tc.wantSources) {
				t.Errorf("Sources = %v, want %v", res.Sources, tc.wantSources)
			}
			if !approx(res.Confidence, tc.wantConf) {
				t.Errorf("Confidence = %v, want %v", res.Confidence, tc.wantConf)
			}
			if res.AIGenerated != (tc.wantConf > 0) {
				t.Errorf("AIGenerated = %t, want %t", res.AIGenerated, tc.wantConf > 0)
			}
		})
	}
}

// TestDetect_DiffHeuristics guards the last-resort source: when nothing
// attests the change, the diff heuristics run, flag the code that reads like
// AI output, and become the only source; plain code in the same change is not
// flagged.
func TestDetect_DiffHeuristics(t *testing.T) {
	_, base := changeRepo(t,
		map[string]string{"handler.go": plainGo},
		map[string]string{
			"handler.go": plainGo + aiLikeSource(),
			"terse.go":   plainGo,
		},
	)

	res, err := Detect(Options{DiffBase: base})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(res.Sources, []string{"diff-heuristics"}) {
		t.Errorf("Sources = %v, want [diff-heuristics]", res.Sources)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "handler.go" {
		t.Fatalf("Files = %+v, want only handler.go flagged", res.Files)
	}
	if len(res.Evidence) != 1 || res.Evidence[0].Signal != "ai-code-patterns" {
		t.Errorf("Evidence = %+v, want one ai-code-patterns row", res.Evidence)
	}
	if !res.AIGenerated || !approx(res.Confidence, res.Files[0].Confidence) {
		t.Errorf("AIGenerated = %t, Confidence = %v, want true at the flagged file's %v",
			res.AIGenerated, res.Confidence, res.Files[0].Confidence)
	}
}

// TestDetect_UnresolvableBase guards the shallow-clone case: when the base
// ref does not exist, trailers are still found by scanning the whole window,
// but with no range diff to cap them there are no per-file counts, and the
// heuristics stay off because a trailer already attests the change.
func TestDetect_UnresolvableBase(t *testing.T) {
	fixtureRepo(t, commitSpec{
		msg:   "feat: x\n\n" + claudeTrailer,
		files: map[string]string{"x.go": aiLikeSource()},
	})

	res, err := Detect(Options{DiffBase: "no-such-ref"})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(res.Sources, []string{"commit-trailer"}) {
		t.Errorf("Sources = %v, want [commit-trailer]", res.Sources)
	}
	if len(res.Files) != 0 {
		t.Errorf("Files = %+v, want none without a range diff", res.Files)
	}
	if !res.AIGenerated || !approx(res.Confidence, 0.9) {
		t.Errorf("AIGenerated = %t, Confidence = %v, want true at 0.9", res.AIGenerated, res.Confidence)
	}
}

// TestDetect_OutsideRepository guards detection where git has nothing to read:
// outside a repository Detect still succeeds and reports no AI, with empty
// (not null) evidence and sources in its JSON, while the signals that need no
// repository, a branch name and a commit message file, still count.
func TestDetect_OutsideRepository(t *testing.T) {
	dir := notARepo(t)

	t.Run("reports nothing, with empty lists in JSON", func(t *testing.T) {
		res, err := Detect(Options{})
		if err != nil {
			t.Fatal(err)
		}
		if res.AIGenerated || res.Confidence != 0 || res.Summary != "No AI code detected" {
			t.Errorf("result = %+v, want no detection", res)
		}
		out, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"evidence":[]`, `"sources":[]`} {
			if !strings.Contains(string(out), want) {
				t.Errorf("JSON = %s, want it to contain %s", out, want)
			}
		}
		if strings.Contains(string(out), `"files"`) {
			t.Errorf("JSON = %s, want the files key omitted", out)
		}
	})

	t.Run("a branch name and a commit message file still count", func(t *testing.T) {
		msg := filepath.Join(dir, "COMMIT_EDITMSG")
		if err := os.WriteFile(msg, []byte("feat: x\n\n"+claudeTrailer+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := Detect(Options{BranchName: "codex/fix-flaky-test", CommitMessageFile: msg})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"commit-trailer", "branch-name"}; !slices.Equal(res.Sources, want) {
			t.Fatalf("Sources = %v, want %v", res.Sources, want)
		}
		if res.Evidence[0].Value != "AI-assisted commit (tool: Claude)" {
			t.Errorf("Value = %q, want a hashless row naming Claude", res.Evidence[0].Value)
		}
		if !res.AIGenerated {
			t.Error("AIGenerated = false, want true")
		}
	})
}
