package detector

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// claudeTrailer is the attribution trailer Claude Code writes.
const claudeTrailer = "Co-Authored-By: Claude <noreply@anthropic.com>"

// fixtureEpoch is the commit time (unix seconds) of a fixture's first commit;
// each later commit is a second newer, so history does not depend on when the
// tests run.
const fixtureEpoch = 1700000000

// isolateGitEnv unsets the variables that make git ignore the working
// directory and use another repository, work tree or index instead. They are
// set when tests run inside a git hook, where the fixture's git commands
// would otherwise act on the real repository.
func isolateGitEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR"} {
		t.Setenv(key, "") // registers the restore of the original value
		os.Unsetenv(key)
	}
}

// notARepo makes an empty directory the working directory for the test, with
// git's upward search for a repository stopped at its parent, and returns it.
func notARepo(t *testing.T) string {
	t.Helper()
	isolateGitEnv(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)
	return dir
}

// commitSpec describes one commit for fixtureRepo.
type commitSpec struct {
	msg    string
	files  map[string]string // created or replaced by the commit
	remove []string          // deleted by the commit
	note   string            // git-ai authorship log attached to the commit (refs/notes/ai)
}

// fixtureRepo creates a repository holding an empty initial commit followed by
// the given commits, makes it the working directory for the test (detection
// runs git in the process's directory) and checks the last commit out. It
// returns the repository and the full hash of every commit, oldest first:
// commits[0] is the initial commit and commits[i+1] is specs[i].
//
// The history is written by one `git fast-import` run, because a git process
// per commit, note and hash lookup would dominate the cost of these tests.
// Messages and notes are stored byte for byte.
func fixtureRepo(t *testing.T, specs ...commitSpec) (dir string, commits []string) {
	t.Helper()
	isolateGitEnv(t)
	dir = t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)

	specs = append([]commitSpec{{msg: "chore: init"}}, specs...)
	var stream, notes strings.Builder
	touchesTree := false
	for i, c := range specs {
		fmt.Fprintf(&stream, "commit refs/heads/main\nmark :%d\ncommitter Test <test@example.com> %d +0000\ndata %d\n%s\n",
			i+1, fixtureEpoch+i, len(c.msg), c.msg)
		for _, name := range slices.Sorted(maps.Keys(c.files)) {
			touchesTree = true
			fmt.Fprintf(&stream, "M 100644 inline %s\ndata %d\n%s\n", name, len(c.files[name]), c.files[name])
		}
		for _, name := range c.remove {
			touchesTree = true
			fmt.Fprintf(&stream, "D %s\n", name)
		}
		stream.WriteString("\n")
		if c.note != "" {
			fmt.Fprintf(&notes, "N inline :%d\ndata %d\n%s\n", i+1, len(c.note), c.note)
		}
	}
	if notes.Len() > 0 {
		fmt.Fprintf(&stream, "commit refs/notes/ai\ncommitter Test <test@example.com> %d +0000\ndata 6\nnotes\n%s\n",
			fixtureEpoch+len(specs), notes.String())
	}

	marks := filepath.Join(dir, ".git", "test-marks")
	cmd := exec.Command("git", "fast-import", "--quiet", "--export-marks="+marks)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stream.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fast-import: %v\n%s", err, out)
	}
	if touchesTree {
		gitIn(t, dir, "reset", "-q", "--hard", "HEAD")
	}

	raw, err := os.ReadFile(marks)
	if err != nil {
		t.Fatalf("read marks: %v", err)
	}
	commits = make([]string, len(specs))
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var n int
		var sha string
		if _, err := fmt.Sscanf(line, ":%d %s", &n, &sha); err != nil || n < 1 || n > len(specs) {
			t.Fatalf("unexpected marks line %q", line)
		}
		commits[n-1] = sha
	}
	return dir, commits
}

// changeRepo builds a repository whose first commit holds the before files
// and whose second commit applies the after files and removes the named
// ones. It returns the repository and the first commit, the diff base that
// sees exactly the second commit's change.
func changeRepo(t *testing.T, before, after map[string]string, remove ...string) (dir, base string) {
	t.Helper()
	dir, c := fixtureRepo(t,
		commitSpec{msg: "chore: before", files: before},
		commitSpec{msg: "feat: after", files: after, remove: remove},
	)
	return dir, c[1]
}

// requireCommits fails the test unless got, a list of abbreviated commit
// hashes as detection reports them, names exactly the full hashes in want, in
// order.
func requireCommits(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("commits = %v, want %d commit(s): %v", got, len(want), want)
	}
	for i := range want {
		if got[i] == "" || !strings.HasPrefix(want[i], got[i]) {
			t.Fatalf("commits[%d] = %q, want an abbreviation of %s", i, got[i], want[i])
		}
	}
}

// splitCommitValue splits an evidence value of the form "AI-assisted commit
// <hash><rest>" into the abbreviated hash and the rest, which keeps its
// leading space.
func splitCommitValue(value string) (hash, rest string) {
	after := strings.TrimPrefix(value, "AI-assisted commit ")
	hash, rest, found := strings.Cut(after, " ")
	if found {
		rest = " " + rest
	}
	return hash, rest
}

// TestDetectFromCommits_Trailers guards the attribution read from commit
// history: which lines mark a commit AI-assisted, which tool and model the
// evidence names (the canonical tool name first, the name as written kept for
// audit), and what is deliberately not attribution.
func TestDetectFromCommits_Trailers(t *testing.T) {
	cases := []struct {
		name   string
		msg    string
		ai     bool   // whether the commit is attributed to AI
		detail string // evidence text following "AI-assisted commit <hash>"
	}{
		{"ai-assisted flag", "feat: a\n\nAI-assisted: true", true, ""},
		{"ai-generated flag is case-insensitive", "feat: a\n\nAI-GENERATED: True", true, ""},
		{"ai-assisted false is not attribution", "feat: a\n\nAI-assisted: false", false, ""},
		{"co-author names the tool", "feat: a\n\n" + claudeTrailer, true, " (tool: Claude)"},
		{
			"co-author model name is kept as written",
			"feat: a\n\nCo-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>",
			true, " (tool: Claude, as written: Claude Sonnet 4.6)",
		},
		{
			"co-author bot account maps to the tool",
			"feat: a\n\nCo-Authored-By: copilot-swe-agent[bot] <1+Copilot@users.noreply.github.com>",
			true, " (tool: GitHub Copilot, as written: copilot-swe-agent[bot])",
		},
		{
			"the first AI co-author names the tool",
			"feat: a\n\nCo-Authored-By: Jane Dev <jane@example.com>\n" + claudeTrailer +
				"\nCo-Authored-By: Gemini <gemini@example.com>",
			true, " (tool: Claude)",
		},
		{"human co-author is not attribution", "feat: a\n\nCo-Authored-By: Aiden Smith <aiden@example.com>", false, ""},
		{"trailer text inside a sentence is not a trailer", "feat: a\n\nWe once wrote Co-Authored-By: Claude <x@y.z> here.", false, ""},
		{
			"assisted-by keeps the model and ignores the analysis tools",
			"feat: a\n\nAssisted-by: Claude:claude-3-opus coccinelle sparse",
			true, " (tool: Claude, model: claude-3-opus)",
		},
		{
			"assisted-by agent alias is canonicalized and kept as written",
			"feat: a\n\nAssisted-by: copilot:gpt-4o",
			true, " (tool: GitHub Copilot, model: gpt-4o, as written: copilot)",
		},
		{"assisted-by without a model", "feat: a\n\nAssisted-by: Gemini", true, " (tool: Gemini)"},
		{"assisted-by with an unknown agent", "feat: a\n\nAssisted-by: Amp:amp-1", true, " (tool: Amp, model: amp-1)"},
		{"assisted-by without an agent is not attribution", "feat: a\n\nAssisted-by:", false, ""},
		{
			"ai-tool of a known tool is canonicalized",
			"feat: a\n\nAI-tool: Claude Code",
			true, " (tool: Claude, as written: Claude Code)",
		},
		{"ai-tool of an unknown tool", "feat: a\n\nAI-tool: Amp", true, " (tool: Amp)"},
		{"ai-tool without a value still marks AI use", "feat: a\n\nAI-tool:", true, ""},
		{"ai-scope accompanies ai-tool", "feat: a\n\nAI-tool: Cursor\nAI-scope: tests", true, " (tool: Cursor) [scope: tests]"},
		{"ai-scope alone is not attribution", "feat: a\n\nAI-scope: tests", false, ""},
		{
			"ai-used-for adds the scope to a named tool",
			"feat: a\n\n" + claudeTrailer + "\nAI-used-for: code, tests",
			true, " (tool: Claude) [scope: code, tests]",
		},
	}

	specs := make([]commitSpec, len(cases))
	for i, tc := range cases {
		specs[i] = commitSpec{msg: tc.msg}
	}
	_, commits := fixtureRepo(t, specs...)
	base := commits[0]

	// One scan over every case's commit. Each evidence row names its commit's
	// hash, which matches the rows back to the cases.
	evidence, hashes := detectFromCommits(Options{DiffBase: base, MaxCommits: len(cases)})
	if len(evidence) != len(hashes) {
		t.Fatalf("%d evidence rows for %d attributed commits", len(evidence), len(hashes))
	}
	rowFor := func(commit string) (row Evidence, short string, found bool) {
		for i, h := range hashes {
			if strings.HasPrefix(commit, h) {
				return evidence[i], h, true
			}
		}
		return Evidence{}, "", false
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, short, found := rowFor(commits[i+1])
			if found != tc.ai {
				t.Fatalf("attributed = %t, want %t (evidence %+v)", found, tc.ai, evidence)
			}
			if !tc.ai {
				return
			}
			want := Evidence{
				Source:     "commit-trailer",
				Signal:     "ai-footer",
				Value:      "AI-assisted commit " + short + tc.detail,
				Confidence: 0.9,
			}
			if row != want {
				t.Errorf("evidence = %+v, want %+v", row, want)
			}
		})
	}
}

// TestAITrailerTool_UnnamedAttribution guards the generic "AI" bucket and its
// limits: an AI-tool trailer with no value and the ai-generated flag
// attribute a commit without naming a tool, while a false flag or an
// Assisted-by with no agent attribute nothing.
func TestAITrailerTool_UnnamedAttribution(t *testing.T) {
	cases := []struct {
		name, msg, want string
	}{
		{"ai-tool without a value", "chore: z\n\nAI-tool:", "AI"},
		{"ai-tool with only whitespace", "chore: z\n\nAI-tool:   ", "AI"},
		{"ai-generated flag", "chore: z\n\nAI-generated: true", "AI"},
		{"ai-generated false", "chore: z\n\nAI-generated: false", ""},
		{"assisted-by without an agent", "chore: z\n\nAssisted-by:", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AITrailerTool(tc.msg); got != tc.want {
				t.Errorf("AITrailerTool(%q) = %q, want %q", tc.msg, got, tc.want)
			}
		})
	}
}

// TestDetectFromCommits_Scope guards the review window: only DiffBase..HEAD
// is evidence for the change, so AI commits already on the target branch are
// not attributed to it. An unresolvable or empty base falls back to the whole
// MaxCommits window, and MaxCommits keeps the newest commits.
func TestDetectFromCommits_Scope(t *testing.T) {
	_, c := fixtureRepo(t,
		commitSpec{msg: "feat: merged earlier\n\n" + claudeTrailer}, // on the target branch already
		commitSpec{msg: "chore: base"},
		commitSpec{msg: "feat: one\n\n" + claudeTrailer},
		commitSpec{msg: "feat: two\n\n" + claudeTrailer},
		commitSpec{msg: "feat: three\n\n" + claudeTrailer},
	)
	old, base, n1, n2, n3 := c[1], c[2], c[3], c[4], c[5]

	cases := []struct {
		name string
		base string
		max  int
		want []string // attributed commits, newest first
	}{
		{"only the commits after the base", base, 10, []string{n3, n2, n1}},
		{"the cap keeps the newest commits", base, 2, []string{n3, n2}},
		{"an unresolvable base scans the whole window", "no-such-ref", 10, []string{n3, n2, n1, old}},
		{"an empty base scans the whole window", "", 10, []string{n3, n2, n1, old}},
		{"the cap applies to the whole window too", "", 3, []string{n3, n2, n1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidence, hashes := detectFromCommits(Options{DiffBase: tc.base, MaxCommits: tc.max})
			requireCommits(t, hashes, tc.want...)
			if len(evidence) != len(tc.want) {
				t.Fatalf("evidence rows = %d, want %d", len(evidence), len(tc.want))
			}
			for i, short := range hashes {
				if !strings.Contains(evidence[i].Value, short) {
					t.Errorf("evidence[%d] = %q, want it to name commit %s", i, evidence[i].Value, short)
				}
			}
		})
	}
}

// TestDetectFromCommits_SkipsMergeCommits guards --no-merges: a merge commit
// whose message carries an AI trailer is not attribution, while the commit it
// merged is.
func TestDetectFromCommits_SkipsMergeCommits(t *testing.T) {
	dir, c := fixtureRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "side")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "feat: side work", "-m", claudeTrailer)
	side := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "checkout", "-q", "main")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "chore: main work")
	gitIn(t, dir, "merge", "--no-ff", "-q", "-m", "Merge branch 'side'", "-m", claudeTrailer, "side")

	evidence, hashes := detectFromCommits(Options{DiffBase: c[0], MaxCommits: 10})

	requireCommits(t, hashes, side)
	if len(evidence) != 1 {
		t.Errorf("evidence rows = %d, want 1 (the merge commit is not attribution)", len(evidence))
	}
}

// TestDetectFromCommits_MessageFile guards the commit-message fallback: the
// file is read only when git history yields nothing to scan (an empty window,
// as in a commit-msg hook, or no repository at all); history takes
// precedence; and an unreadable or blank file attributes nothing. A message
// read from a file has no commit hash.
func TestDetectFromCommits_MessageFile(t *testing.T) {
	writeMsg := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "COMMIT_EDITMSG")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	aiMessage := "feat: x\n\n" + claudeTrailer + "\n"

	t.Run("an empty window falls back to the file", func(t *testing.T) {
		fixtureRepo(t)
		file := writeMsg(t, aiMessage)

		evidence, hashes := detectFromCommits(Options{DiffBase: "HEAD", CommitMessageFile: file, MaxCommits: 10})

		if len(evidence) != 1 || evidence[0].Value != "AI-assisted commit (tool: Claude)" {
			t.Errorf("evidence = %+v, want one hashless row naming Claude", evidence)
		}
		if len(hashes) != 0 {
			t.Errorf("hashes = %v, want none for a message read from a file", hashes)
		}
	})

	t.Run("no repository falls back to the file", func(t *testing.T) {
		notARepo(t)
		file := writeMsg(t, aiMessage)

		evidence, hashes := detectFromCommits(Options{CommitMessageFile: file, MaxCommits: 10})

		if len(evidence) != 1 || len(hashes) != 0 {
			t.Errorf("evidence = %+v, hashes = %v, want one row and no hashes", evidence, hashes)
		}
	})

	t.Run("history takes precedence over the file", func(t *testing.T) {
		_, c := fixtureRepo(t, commitSpec{msg: "feat: human work"})
		file := writeMsg(t, aiMessage)

		evidence, _ := detectFromCommits(Options{DiffBase: c[0], CommitMessageFile: file, MaxCommits: 10})

		if len(evidence) != 0 {
			t.Errorf("evidence = %+v, want none: the scanned commits are human and the file is only a fallback", evidence)
		}
	})

	t.Run("a missing or blank file attributes nothing", func(t *testing.T) {
		notARepo(t)
		for name, file := range map[string]string{
			"missing": filepath.Join(t.TempDir(), "absent"),
			"blank":   writeMsg(t, " \n\t\n"),
		} {
			evidence, hashes := detectFromCommits(Options{CommitMessageFile: file, MaxCommits: 10})
			if len(evidence) != 0 || len(hashes) != 0 {
				t.Errorf("%s file: evidence = %+v, hashes = %v, want none", name, evidence, hashes)
			}
		}
	})
}

// TestDetectFromCommits_EmptyMessage guards the record parser against a
// commit with an empty message: it yields no evidence row and does not
// disturb the commits around it.
func TestDetectFromCommits_EmptyMessage(t *testing.T) {
	_, c := fixtureRepo(t,
		commitSpec{msg: "feat: before\n\n" + claudeTrailer},
		commitSpec{msg: ""},
		commitSpec{msg: "feat: after\n\n" + claudeTrailer},
	)

	evidence, hashes := detectFromCommits(Options{DiffBase: c[0], MaxCommits: 10})

	requireCommits(t, hashes, c[3], c[1])
	if len(evidence) != 2 {
		t.Errorf("evidence rows = %d, want 2", len(evidence))
	}
}

// TestDetectFromCommits_RecordSeparatorInMessage guards the record parser
// against a message containing the 0x1e separator it frames commits with: the
// stray tail is not a commit, so it must neither panic the parser nor be
// reported as one, and the commit's own trailer still counts.
func TestDetectFromCommits_RecordSeparatorInMessage(t *testing.T) {
	msg := "feat: x\n\n" + claudeTrailer + "\n\nnotes\x1ea stray tail\nCo-Authored-By: Gemini <gemini@example.com>\n"
	_, c := fixtureRepo(t, commitSpec{msg: msg})

	evidence, hashes := detectFromCommits(Options{DiffBase: c[0], MaxCommits: 10})

	requireCommits(t, hashes, c[1])
	if len(evidence) != 1 {
		t.Fatalf("evidence rows = %d, want 1", len(evidence))
	}
	if want := "AI-assisted commit " + hashes[0] + " (tool: Claude)"; evidence[0].Value != want {
		t.Errorf("evidence = %q, want %q", evidence[0].Value, want)
	}
}
