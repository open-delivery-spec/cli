package gitai

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fixtureEpoch is the commit time (unix seconds) of a fixture's first commit;
// each later commit is a second newer, so history does not depend on when the
// tests run.
const fixtureEpoch = 1700000000

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

// notARepo makes an empty directory the working directory for the test and
// stops git's upward search for a repository at its parent.
func notARepo(t *testing.T) {
	t.Helper()
	isolateGitEnv(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)
}

// commitSpec describes one commit for fixtureRepo.
type commitSpec struct {
	msg     string
	files   map[string]string // created or replaced by the commit
	note    string            // authorship log attached to the commit
	noteRef string            // notes ref for note; refs/notes/ai when empty
}

// fixtureRepo creates a repository holding the given commits, oldest first,
// and makes it the working directory for the test: ReadRange runs git in the
// process's directory. With no specs the repository has no commits. It returns
// the repository and the full hash of every commit.
//
// The history and its notes are written by one `git fast-import` run, because
// a git process per commit and note would dominate the cost of these tests.
// Notes are stored byte for byte. The work tree is left as `git init` made it:
// ReadRange reads only history and notes.
func fixtureRepo(t *testing.T, specs ...commitSpec) (dir string, commits []string) {
	t.Helper()
	isolateGitEnv(t)
	dir = t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	if len(specs) == 0 {
		return dir, nil
	}

	var stream strings.Builder
	notes := map[string]*strings.Builder{} // notes ref -> its note entries
	for i, c := range specs {
		fmt.Fprintf(&stream, "commit refs/heads/main\nmark :%d\ncommitter Test <test@example.com> %d +0000\ndata %d\n%s\n",
			i+1, fixtureEpoch+i, len(c.msg), c.msg)
		for _, name := range slices.Sorted(maps.Keys(c.files)) {
			fmt.Fprintf(&stream, "M 100644 inline %s\ndata %d\n%s\n", name, len(c.files[name]), c.files[name])
		}
		stream.WriteString("\n")
		if c.note != "" {
			ref := c.noteRef
			if ref == "" {
				ref = "refs/notes/ai" // the namespace the Git AI Standard mandates
			}
			if notes[ref] == nil {
				notes[ref] = &strings.Builder{}
			}
			fmt.Fprintf(notes[ref], "N inline :%d\ndata %d\n%s\n", i+1, len(c.note), c.note)
		}
	}
	for n, ref := range slices.Sorted(maps.Keys(notes)) {
		fmt.Fprintf(&stream, "commit %s\ncommitter Test <test@example.com> %d +0000\ndata 6\nnotes\n%s\n",
			ref, fixtureEpoch+len(specs)+n, notes[ref].String())
	}

	marks := filepath.Join(dir, ".git", "test-marks")
	cmd := exec.Command("git", "fast-import", "--quiet", "--export-marks="+marks)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stream.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fast-import: %v\n%s", err, out)
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

// authorshipLog renders an authorship log: the attestation section, the "---"
// separator and metadata that declares the given sessions (see sessionJSON).
func authorshipLog(attestations string, sessions ...string) string {
	return strings.Trim(attestations, "\n") + "\n---\n" +
		`{"schema_version":"authorship/3.0.0","sessions":{` + strings.Join(sessions, ",") + `}}`
}

// sessionJSON renders one entry of the metadata's sessions object.
func sessionJSON(id, tool, model string) string {
	return fmt.Sprintf(`%q:{"agent_id":{"tool":%q,"model":%q}}`, id, tool, model)
}

// TestReadRange_AggregatesNotes guards the range read end to end against real
// notes: commits with usable notes are reported newest first with their short
// hash, per-file AI lines are summed across the range, agents are unique and
// sorted (one agent appears in two commits), and a commit with no note or a
// malformed one is skipped instead of failing the read. The range is also
// bounded by the base ref and by the commit cap, which counts commits
// scanned, not notes found.
func TestReadRange_AggregatesNotes(t *testing.T) {
	dir, c := fixtureRepo(t,
		commitSpec{
			msg:   "add a",
			files: map[string]string{"a.go": "1\n2\n3\n"},
			note:  authorshipLog("a.go\n  s_aaa::t_1 1-3", sessionJSON("s_aaa", "cursor", "gpt-4")),
		},
		commitSpec{msg: "add b", files: map[string]string{"b.go": "1\n2\n"}}, // no note
		commitSpec{
			msg:   "edit a and b",
			files: map[string]string{"a.go": "1\n2\n3\n4\n", "b.go": "1\n2\n3\n4\n5\n"},
			note: authorshipLog(`
a.go
  s_bbb::t_2 1-2
  h_0123456789ab 3-4
b.go
  0123456789abcdef 2-3
  s_ccc::t_3 4
  s_ddd::t_4 5`,
				sessionJSON("s_bbb", "claude", ""),
				sessionJSON("s_ccc", "cursor", "gpt-4"), // the first commit's agent again
				sessionJSON("s_ddd", "aider", "gpt-4o"),
			),
		},
		commitSpec{msg: "add c", files: map[string]string{"c.go": "1\n"}, note: "this is not an authorship log"},
	)

	short := func(rev string) string { return gitIn(t, dir, "rev-parse", "--short", rev) }
	wantC1 := CommitAttribution{
		Hash:    short(c[0]),
		AILines: 3,
		Files:   map[string]int{"a.go": 3},
		Agents:  []string{"cursor/gpt-4"},
	}
	wantC3 := CommitAttribution{
		Hash:       short(c[2]),
		AILines:    6, // 2 session lines in a.go; in b.go 2 legacy-key lines and 1 line from each of 2 sessions
		HumanLines: 2,
		Files:      map[string]int{"a.go": 2, "b.go": 4},
		Agents:     []string{"aider/gpt-4o", "claude", "cursor/gpt-4"}, // the legacy key has no label
	}
	both := &RangeAttribution{
		Commits: []CommitAttribution{wantC3, wantC1},
		Files:   map[string]int{"a.go": 5, "b.go": 4},
		Agents:  []string{"aider/gpt-4o", "claude", "cursor/gpt-4"},
	}
	onlyC3 := &RangeAttribution{
		Commits: []CommitAttribution{wantC3},
		Files:   map[string]int{"a.go": 2, "b.go": 4},
		Agents:  []string{"aider/gpt-4o", "claude", "cursor/gpt-4"},
	}

	cases := []struct {
		name string
		base string
		max  int
		runs int // repeats, for results that depend on map order; 0 means once
		want *RangeAttribution
	}{
		{"whole history", "", 10, 3, both},
		{"base excludes the commits it already contains", c[0], 10, 0, onlyC3},
		{"unresolvable base scans the whole window", "no-such-ref", 10, 0, both},
		{"cap counts the malformed newest commit", "", 2, 0, onlyC3},
		{"cap leaves out older notes", "", 1, 0, nil},
		{"zero cap scans nothing", "", 0, 0, nil},
		{"empty range", "HEAD", 10, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for range max(tc.runs, 1) {
				got, err := ReadRange(tc.base, tc.max)
				if err != nil {
					t.Fatalf("ReadRange(%q, %d): %v", tc.base, tc.max, err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("ReadRange(%q, %d) = %+v, want %+v", tc.base, tc.max, got, tc.want)
				}
			}
		})
	}
}

// TestReadRange_NoAttribution guards the common case on repositories that do
// not use git-ai: whatever is missing or unusable, ReadRange reports no
// attribution (nil, nil) and never an error, so detection falls through to
// the other signal sources.
func TestReadRange_NoAttribution(t *testing.T) {
	validLog := authorshipLog("a.go\n  s_1::t_1 1", sessionJSON("s_1", "cursor", "m"))
	oneCommit := func(note, noteRef string) func(t *testing.T) {
		return func(t *testing.T) {
			fixtureRepo(t, commitSpec{msg: "add a", files: map[string]string{"a.go": "1\n"}, note: note, noteRef: noteRef})
		}
	}

	cases := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"not a repository", notARepo},
		{"repository without commits", func(t *testing.T) { fixtureRepo(t) }},
		{"commits without notes", oneCommit("", "")},
		{"notes in another namespace", oneCommit(validLog, "refs/notes/commits")},
		{"only malformed notes", oneCommit("not an authorship log", "")},
		{"empty note", func(t *testing.T) {
			dir, c := fixtureRepo(t, commitSpec{msg: "add a", files: map[string]string{"a.go": "1\n"}})
			gitIn(t, dir, "notes", "--ref=ai", "add", "--allow-empty", "-m", "", c[0])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			got, err := ReadRange("", 10)
			if err != nil || got != nil {
				t.Errorf("ReadRange = (%+v, %v), want (nil, nil)", got, err)
			}
		})
	}
}

// TestReadRange_InitialCommit guards the unscoped fallback for a repository
// whose only commit has no parent: the default base HEAD~1 does not resolve,
// and the commit's own note must still be read.
func TestReadRange_InitialCommit(t *testing.T) {
	fixtureRepo(t, commitSpec{
		msg:   "add a",
		files: map[string]string{"a.go": "1\n2\n"},
		note:  authorshipLog("a.go\n  s_1::t_1 1-2", sessionJSON("s_1", "cursor", "m")),
	})

	got, err := ReadRange("HEAD~1", 10)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if got == nil || len(got.Commits) != 1 || got.Files["a.go"] != 2 {
		t.Fatalf("ReadRange = %+v, want the initial commit with 2 AI lines in a.go", got)
	}
}

// TestReadRange_SkipsMergeCommits guards the --no-merges scan: a note on a
// merge commit is not attribution for the change, while the notes on the
// commits it merged still are.
func TestReadRange_SkipsMergeCommits(t *testing.T) {
	dir, _ := fixtureRepo(t, commitSpec{msg: "add a", files: map[string]string{"a.go": "1\n"}})
	gitIn(t, dir, "reset", "-q", "--hard", "HEAD") // the fixture leaves the work tree empty; the commits below need it in sync
	log := func(file string, lines int) string {
		return authorshipLog(fmt.Sprintf("%s\n  s_1::t_1 1-%d", file, lines), sessionJSON("s_1", "cursor", "m"))
	}
	gitIn(t, dir, "checkout", "-q", "-b", "side")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "side work")
	side := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "checkout", "-q", "main")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "main work")
	gitIn(t, dir, "merge", "--no-ff", "-q", "-m", "merge side", "side")

	gitIn(t, dir, "notes", "--ref=ai", "add", "-m", log("m.go", 9), "HEAD") // the merge commit
	if got, err := ReadRange("", 10); err != nil || got != nil {
		t.Fatalf("ReadRange with a note only on the merge commit = (%+v, %v), want (nil, nil)", got, err)
	}

	gitIn(t, dir, "notes", "--ref=ai", "add", "-m", log("b.go", 2), side)
	got, err := ReadRange("", 10)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if got == nil || len(got.Commits) != 1 || got.Files["b.go"] != 2 || got.Files["m.go"] != 0 {
		t.Errorf("ReadRange = %+v, want only the merged commit's 2 AI lines in b.go", got)
	}
}

// TestGitOutput guards the git wrapper: stdout comes back on success, and a
// failing command (bad revision, or no repository at all) is an error with no
// output, which is what lets ReadRange treat "no such note" as absence.
func TestGitOutput(t *testing.T) {
	t.Run("in a repository", func(t *testing.T) {
		_, c := fixtureRepo(t, commitSpec{msg: "add a"})

		out, err := gitOutput("rev-parse", "HEAD")
		if err != nil || strings.TrimSpace(out) != c[0] {
			t.Errorf("gitOutput(rev-parse HEAD) = (%q, %v), want %q", out, err, c[0])
		}
		out, err = gitOutput("rev-parse", "--verify", "no-such-ref")
		if err == nil || out != "" {
			t.Errorf("gitOutput(rev-parse --verify no-such-ref) = (%q, %v), want an error and no output", out, err)
		}
	})
	t.Run("without a repository", func(t *testing.T) {
		notARepo(t)
		if out, err := gitOutput("rev-parse", "HEAD"); err == nil || out != "" {
			t.Errorf("gitOutput outside a repository = (%q, %v), want an error and no output", out, err)
		}
	})
}
