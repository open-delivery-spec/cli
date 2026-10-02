package detector

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/open-delivery-spec/cli/internal/gitai"
)

// plainGo is terse, ordinary code that no heuristic scores.
const plainGo = "package p\n\nfunc a() int { return 1 }\nfunc b() int { return 2 }\n"

// TestRangeInsertions guards the per-path insertion counts every per-file AI
// count is capped at: a path maps to the lines the range adds to it, a path
// the range only shrinks or deletes maps to zero, and a binary file (numstat
// prints "-"), an unchanged file, a clean range and an unresolvable base all
// contribute nothing.
func TestRangeInsertions(t *testing.T) {
	_, base := changeRepo(t,
		map[string]string{
			"edited.go":  "a\nb\nc\n",
			"trimmed.go": "a\nb\nc\nd\n",
			"removed.go": "x\ny\n",
			"same.go":    "s\n",
			"blob.bin":   "\x00\x01",
		},
		map[string]string{
			"edited.go":  "a\nb\nc\nd\ne\n",
			"trimmed.go": "a\nb\n",
			"added.go":   "1\n2\n3\n",
			"blob.bin":   "\x00\x02\x03",
		},
		"removed.go",
	)

	t.Run("counts the lines each path gains", func(t *testing.T) {
		want := map[string]int{"edited.go": 2, "trimmed.go": 0, "removed.go": 0, "added.go": 3}
		if got := rangeInsertions(base); !reflect.DeepEqual(got, want) {
			t.Errorf("rangeInsertions = %v, want %v (binary and unchanged files absent)", got, want)
		}
	})
	t.Run("a clean range has no insertions", func(t *testing.T) {
		if got := rangeInsertions("HEAD"); len(got) != 0 {
			t.Errorf("rangeInsertions(HEAD) = %v, want none", got)
		}
	})
	t.Run("an unresolvable base has no insertions", func(t *testing.T) {
		if got := rangeInsertions("no-such-ref"); len(got) != 0 {
			t.Errorf("rangeInsertions(no-such-ref) = %v, want none", got)
		}
	})
}

// TestFilesFromCommits_Skips guards which attested lines are reported as AI
// code. Counts add up across attributed commits and rows come out sorted by
// path; but AI lines the final change no longer contains (the file shrank
// back, or was deleted), an attributed commit that only removed lines, binary
// content, non-code files and a hash git cannot show must not produce a row
// or abort the others.
func TestFilesFromCommits_Skips(t *testing.T) {
	_, c := fixtureRepo(t,
		commitSpec{msg: "chore: before", files: map[string]string{
			"shrink.go": "s1\ns2\ns3\ns4\ns5\n",
			"del.go":    "d1\nd2\nd3\nd4\n",
		}},
		commitSpec{msg: "feat: ai one\n\n" + claudeTrailer, files: map[string]string{
			"keep.go":   "k1\nk2\nk3\n",                     // +3, survives
			"zeta.go":   "z1\nz2\n",                         // +2, survives
			"mid.go":    "m1\nm2\nm3\nm4\n",                 // +4, survives
			"alpha.go":  "a1\n",                             // +1, survives
			"shrink.go": "s1\ns2\ns3\ns4\ns5\na1\na2\na3\n", // +3, trimmed again below
			"del.go":    "d1\nd2\n",                         // only removes lines
			"tmp.go":    "t1\nt2\n",                         // +2, deleted below
			"blob.go":   "\x00\x01\x02 binary",              // numstat prints "-"
			"notes.md":  "n1\nn2\n",                         // not code
		}},
		commitSpec{msg: "feat: ai two\n\n" + claudeTrailer, files: map[string]string{
			"keep.go": "k1\nk2\nk3\nk4\nk5\n", // +2 more in the same file
		}},
		commitSpec{
			msg: "refactor: human cleanup",
			files: map[string]string{
				"shrink.go": "s1\ns2\n",         // range diff only deletes lines
				"del.go":    "d1\nd2\ne1\ne2\n", // range diff adds 2, none of them the AI commit's
			},
			remove: []string{"tmp.go"},
		},
	)
	base, ai1, ai2 := c[1], c[2], c[3]
	want := []FileDetection{
		{Path: "alpha.go", AILines: 1, TotalLines: 1, Confidence: 0.9},
		{Path: "keep.go", AILines: 5, TotalLines: 5, Confidence: 0.9},
		{Path: "mid.go", AILines: 4, TotalLines: 4, Confidence: 0.9},
		{Path: "zeta.go", AILines: 2, TotalLines: 2, Confidence: 0.9},
	}

	t.Run("reports only code lines the final change still contains, sorted by path", func(t *testing.T) {
		// Repeated because the order of a map is random: an unsorted result
		// would match the expected order by chance now and then.
		for range 3 {
			if got := filesFromCommits([]string{ai1, ai2}, base); !reflect.DeepEqual(got, want) {
				t.Fatalf("filesFromCommits = %+v, want %+v", got, want)
			}
		}
	})
	t.Run("a hash git cannot show is skipped", func(t *testing.T) {
		bogus := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
		if got := filesFromCommits([]string{bogus, ai1, ai2}, base); !reflect.DeepEqual(got, want) {
			t.Errorf("filesFromCommits = %+v, want %+v", got, want)
		}
	})
	t.Run("an unresolvable base leaves nothing to attribute", func(t *testing.T) {
		if got := filesFromCommits([]string{ai1, ai2}, "no-such-ref"); len(got) != 0 {
			t.Errorf("filesFromCommits = %+v, want none without a range diff to cap at", got)
		}
	})
}

// TestFilesFromGitAI guards the conversion of git-ai's measured line counts:
// rows are sorted by path, carry the measured confidence, take their total
// from the range diff's insertions, cap the AI lines at that total, and leave
// out files the diff does not add lines to (authorship recorded outside this
// change, or on a file the range only shrank).
func TestFilesFromGitAI(t *testing.T) {
	_, base := changeRepo(t,
		map[string]string{"shrunk.go": "a\nb\nc\nd\n"},
		map[string]string{
			"a.go":      "1\n2\n3\n4\n",
			"b.go":      "1\n2\n",
			"z.go":      "1\n",
			"shrunk.go": "a\nb\n",
		},
	)
	attr := &gitai.RangeAttribution{Files: map[string]int{
		"z.go":      1,
		"a.go":      3,
		"b.go":      10, // more than the 2 lines the change adds
		"gone.go":   5,  // not in the diff
		"shrunk.go": 2,  // in the diff, but it adds no lines
	}}

	t.Run("converts measured counts", func(t *testing.T) {
		want := []FileDetection{
			{Path: "a.go", AILines: 3, TotalLines: 4, Confidence: 0.95},
			{Path: "b.go", AILines: 2, TotalLines: 2, Confidence: 0.95},
			{Path: "z.go", AILines: 1, TotalLines: 1, Confidence: 0.95},
		}
		// Repeated because the order of a map is random: an unsorted result
		// would match the expected order by chance now and then.
		for range 6 {
			if got := filesFromGitAI(attr, base); !reflect.DeepEqual(got, want) {
				t.Fatalf("filesFromGitAI = %+v, want %+v", got, want)
			}
		}
	})
	t.Run("an unresolvable base leaves nothing to attribute", func(t *testing.T) {
		if got := filesFromGitAI(attr, "no-such-ref"); len(got) != 0 {
			t.Errorf("filesFromGitAI = %+v, want none without a range diff", got)
		}
	})
	t.Run("no measured files yield no rows", func(t *testing.T) {
		if got := filesFromGitAI(&gitai.RangeAttribution{}, base); len(got) != 0 {
			t.Errorf("filesFromGitAI = %+v, want none", got)
		}
	})
}

// TestDetectFromDiff_ScoresAddedLines guards the heuristic scan of a real
// diff: only the lines a change adds are scored (not the code around them),
// code files at or over the 0.4 threshold become rows whose AI share is that
// score, files under it (including one that scores 0.27) and non-code files
// (even ones that read like AI code) are left out, and one evidence row counts
// the flagged files.
func TestDetectFromDiff_ScoresAddedLines(t *testing.T) {
	// Five comments over four uniformly indented lines: comment density
	// (0.9 x 0.3) plus indentation (0.7 x 0.2) is 0.41, just over the cutoff.
	borderline := []string{
		"// first note", "// second note", "// third note", "// fourth note", "// fifth note",
		"    a := 1", "    b := 2", "    c := 3", "    d := 4",
	}
	_, base := changeRepo(t,
		map[string]string{"handler.go": plainGo, "aaa_old.go": "1\n2\n3\n4\n"},
		map[string]string{
			"aaa_old.go":    "1\n2\n",                 // only deletes lines: nothing to score, and the scan goes on
			"handler.go":    plainGo + aiLikeSource(), // appends 20 AI-like lines to existing code
			"borderline.go": strings.Join(borderline, "\n") + "\n",
			"commented.go":  "// one\n// two\n// three\n// four\n", // comments alone score 0.27
			"terse.go":      plainGo,
			"notes.txt":     aiLikeSource(), // reads like AI code, but is not a code file
		},
	)

	files, evidence := detectFromDiff(base)

	wantLines := map[string][]string{"borderline.go": borderline, "handler.go": aiLikeLines()}
	if len(files) != len(wantLines) {
		t.Fatalf("files = %+v, want only borderline.go and handler.go", files)
	}
	for _, f := range files {
		lines, ok := wantLines[f.Path]
		if !ok {
			t.Errorf("unexpected file flagged: %+v", f)
			continue
		}
		if f.TotalLines != len(lines) {
			t.Errorf("%s: TotalLines = %d, want the %d lines the change adds, not the whole file", f.Path, f.TotalLines, len(lines))
		}
		if want := scoreAIPatterns(lines); !approx(f.Confidence, want) || f.Confidence < 0.4 {
			t.Errorf("%s: Confidence = %v, want the heuristic score of the added lines, %v", f.Path, f.Confidence, want)
		}
		if want := int(float64(f.TotalLines) * f.Confidence); f.AILines != want || f.AILines == 0 {
			t.Errorf("%s: AILines = %d, want the score's share of the added lines, %d", f.Path, f.AILines, want)
		}
	}
	wantEvidence := []Evidence{{
		Source:     "diff-heuristics",
		Signal:     "ai-code-patterns",
		Value:      "2 file(s) match AI code patterns",
		Confidence: 0.4,
	}}
	if !reflect.DeepEqual(evidence, wantEvidence) {
		t.Errorf("evidence = %+v, want %+v", evidence, wantEvidence)
	}
}

// TestDetectFromDiff_NothingToScore guards the early exits: a range git
// cannot diff, a clean range, a change to non-code files only, and a change
// that only deletes lines all yield no rows and no evidence.
func TestDetectFromDiff_NothingToScore(t *testing.T) {
	check := func(t *testing.T, base string) {
		t.Helper()
		files, evidence := detectFromDiff(base)
		if len(files) != 0 || len(evidence) != 0 {
			t.Errorf("detectFromDiff(%q) = (%+v, %+v), want nothing", base, files, evidence)
		}
	}

	t.Run("non-code changes, a clean range and a bad base", func(t *testing.T) {
		_, base := changeRepo(t,
			map[string]string{"README.md": "a\n"},
			map[string]string{"README.md": "a\nb\n", "notes.txt": aiLikeSource()},
		)
		check(t, base)
		check(t, "HEAD")
		check(t, "no-such-ref")
	})
	t.Run("a change that only deletes lines", func(t *testing.T) {
		_, base := changeRepo(t,
			map[string]string{"old.go": "1\n2\n3\n4\n5\n6\n"},
			map[string]string{"old.go": "1\n2\n3\n"},
		)
		check(t, base)
	})
}

// TestDetectFromDiff_SkipsFileGitCannotDiff guards the per-file loop: a
// changed file whose diff git cannot render is skipped, and the files after
// it are still scored. A textconv driver that cannot run makes `git diff`
// fail for broken.go alone, while the change list still names it first.
func TestDetectFromDiff_SkipsFileGitCannotDiff(t *testing.T) {
	dir, base := changeRepo(t,
		map[string]string{"broken.go": plainGo},
		map[string]string{
			"broken.go":  plainGo + aiLikeSource(),
			"handler.go": aiLikeSource(),
		},
	)
	gitIn(t, dir, "config", "diff.unrunnable.textconv", "/nonexistent/textconv")
	attributes := filepath.Join(dir, ".git", "info", "attributes")
	if err := os.MkdirAll(filepath.Dir(attributes), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attributes, []byte("broken.go diff=unrunnable\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, _ := detectFromDiff(base)

	if len(files) != 1 || files[0].Path != "handler.go" {
		t.Errorf("files = %+v, want only handler.go: broken.go is skipped, not fatal", files)
	}
}
