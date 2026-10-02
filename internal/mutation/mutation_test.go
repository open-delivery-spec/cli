package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sampleReport = `{
  "go_module": "github.com/example/proj",
  "test_efficacy": 66.7,
  "files": [
    {
      "file_name": "internal/svc/add.go",
      "mutations": [
        {"line": 4, "column": 10, "type": "ARITHMETIC_BASE", "status": "KILLED"},
        {"line": 8, "column": 10, "type": "CONDITIONALS_BOUNDARY", "status": "LIVED"},
        {"line": 12, "column": 3, "type": "INVERT_NEGATIVES", "status": "TIMED OUT"},
        {"line": 20, "column": 1, "type": "ARITHMETIC_BASE", "status": "NOT COVERED"},
        {"line": 22, "column": 1, "type": "ARITHMETIC_BASE", "status": "NOT VIABLE"}
      ]
    },
    {
      "file_name": "internal/other/x.go",
      "mutations": [
        {"line": 5, "column": 1, "type": "ARITHMETIC_BASE", "status": "LIVED"}
      ]
    }
  ]
}`

func TestParse(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gremlins.json")
	if err := os.WriteFile(p, []byte(sampleReport), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Parse(p)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(r.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(r.Files))
	}
	if r.Files[0].FileName != "internal/svc/add.go" || len(r.Files[0].Mutations) != 5 {
		t.Errorf("unexpected first file: %+v", r.Files[0])
	}
}

func TestParse_invalid(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.json")
	os.WriteFile(p, []byte("{not json"), 0o644)
	if _, err := Parse(p); err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestClassify(t *testing.T) {
	for _, s := range []string{"KILLED", "killed", "TIMED OUT", "TIMEDOUT"} {
		if !killed(s) {
			t.Errorf("killed(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"LIVED", "live"} {
		if !lived(s) {
			t.Errorf("lived(%q) = false, want true", s)
		}
	}
	// Excluded statuses are neither killed nor lived.
	for _, s := range []string{"NOT COVERED", "NOT VIABLE", "RUNNABLE"} {
		if killed(s) || lived(s) {
			t.Errorf("%q should be neither killed nor lived", s)
		}
	}
}

func TestDiffScopedMSI_scopesToAddedLines(t *testing.T) {
	r := &Report{Files: []FileMutations{
		{FileName: "internal/svc/add.go", Mutations: []Mutation{
			{Line: 4, Status: "KILLED"},       // added → killed
			{Line: 8, Status: "LIVED"},        // added → lived (escape)
			{Line: 12, Status: "TIMED OUT"},   // added → counts as killed
			{Line: 20, Status: "NOT COVERED"}, // added but excluded from denom
			{Line: 22, Status: "NOT VIABLE"},  // added but excluded from denom
			{Line: 99, Status: "LIVED"},       // NOT an added line → ignored
		}},
	}}
	// add.go added lines: 4, 8, 12, 20, 22 (not 99). Path is repo-relative and
	// matches the report's module-relative path exactly.
	added := map[string][]int{"internal/svc/add.go": {4, 8, 12, 20, 22}}
	killedN, total := r.DiffScopedMSI(added)
	if killedN != 2 || total != 3 { // killed: 4,12 ; lived: 8 ; not-covered/viable excluded; 99 out of scope
		t.Fatalf("MSI = %d/%d, want 2/3", killedN, total)
	}
}

func TestDiffScopedMSI_suffixMatch(t *testing.T) {
	// Report path is module-qualified; diff path is repo-relative.
	r := &Report{Files: []FileMutations{
		{FileName: "github.com/example/proj/internal/svc/add.go", Mutations: []Mutation{
			{Line: 4, Status: "KILLED"},
			{Line: 8, Status: "LIVED"},
		}},
	}}
	added := map[string][]int{"internal/svc/add.go": {4, 8}}
	killedN, total := r.DiffScopedMSI(added)
	if killedN != 1 || total != 2 {
		t.Fatalf("MSI = %d/%d, want 1/2", killedN, total)
	}
}

func TestDiffScopedMSI_unmatchedFileContributesNothing(t *testing.T) {
	r := &Report{Files: []FileMutations{
		{FileName: "internal/unrelated/y.go", Mutations: []Mutation{
			{Line: 4, Status: "LIVED"},
		}},
	}}
	added := map[string][]int{"internal/svc/add.go": {4}}
	killedN, total := r.DiffScopedMSI(added)
	if killedN != 0 || total != 0 {
		t.Fatalf("MSI = %d/%d, want 0/0 (no matching file)", killedN, total)
	}
}

func TestDiffScopedMSI_nilReport(t *testing.T) {
	var r *Report
	killedN, total := r.DiffScopedMSI(map[string][]int{"a.go": {1}})
	if killedN != 0 || total != 0 {
		t.Fatalf("nil report should yield 0/0, got %d/%d", killedN, total)
	}
}

// TestParse_missingFile guards the read failure: a report path that does not
// exist is returned as the underlying open error, with no report.
func TestParse_missingFile(t *testing.T) {
	r, err := Parse(filepath.Join(t.TempDir(), "absent.json"))
	if !errors.Is(err, os.ErrNotExist) || r != nil {
		t.Fatalf("Parse(missing) = (%v, %v), want (nil, an os.ErrNotExist error)", r, err)
	}
}

// TestParse_malformedReports guards the decode failures: each is an error that
// names the report file, while an empty JSON object is a valid report with no
// files (nothing to score, not a parse failure).
func TestParse_malformedReports(t *testing.T) {
	bad := map[string]string{
		"empty file":               "",
		"truncated document":       `{"files":[`,
		"files has the wrong type": `{"files":"none"}`,
		"line is not a number":     `{"files":[{"file_name":"a.go","mutations":[{"line":"four","status":"KILLED"}]}]}`,
	}
	for name, content := range bad {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "gremlins.json")
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			r, err := Parse(p)
			if err == nil || r != nil {
				t.Fatalf("Parse = (%v, %v), want (nil, an error)", r, err)
			}
			if !strings.Contains(err.Error(), "parsing mutation report") || !strings.Contains(err.Error(), p) {
				t.Errorf("error = %q, want it to say it was parsing the report %s", err, p)
			}
		})
	}

	t.Run("empty object", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "gremlins.json")
		if err := os.WriteFile(p, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := Parse(p)
		if err != nil || r == nil || len(r.Files) != 0 {
			t.Errorf("Parse({}) = (%+v, %v), want an empty report", r, err)
		}
	})
}

// TestClassify_caseAndEmpty guards status matching: it ignores case and spacing
// of the keyword, and an empty status is neither killed nor lived.
func TestClassify_caseAndEmpty(t *testing.T) {
	for _, s := range []string{"Killed", "Timed Out", "timed out"} {
		if !killed(s) {
			t.Errorf("killed(%q) = false, want true", s)
		}
	}
	if !lived("Lived") {
		t.Error(`lived("Lived") = false, want true`)
	}
	if killed("") || lived("") {
		t.Error("an empty status should be neither killed nor lived")
	}
}

// TestBasename guards the last path element used for the fallback match.
func TestBasename(t *testing.T) {
	cases := map[string]string{
		"a/b/c.go": "c.go",
		"c.go":     "c.go",
		"/c.go":    "c.go",
		"a/":       "",
		"":         "",
	}
	for in, want := range cases {
		if got := basename(in); got != want {
			t.Errorf("basename(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMatchFile guards how a report path finds its diff file: exact name, a
// module-qualified report path, a report path shorter than the diff path, then
// a unique basename as the last resort. Of several diff paths that are all
// suffixes of the report path the most specific (longest) wins; suffixes must
// align on a path separator; a basename shared by several diff files is
// ambiguous and matches nothing. The result is the set of added lines. Wherever
// a primary rule is under test a same-named distractor is present, so the
// basename fallback cannot stand in for it.
func TestMatchFile(t *testing.T) {
	cases := []struct {
		name    string
		report  string
		added   map[string][]int
		wantKey string // diff path whose lines are expected; "" = no match
	}{
		{
			"exact path",
			"internal/svc/add.go",
			map[string][]int{"internal/svc/add.go": {4, 8}, "other/add.go": {1}},
			"internal/svc/add.go",
		},
		{
			"module-qualified report path",
			"github.com/example/proj/internal/svc/add.go",
			map[string][]int{"internal/svc/add.go": {4, 8}, "other/add.go": {1}},
			"internal/svc/add.go",
		},
		{
			"report path shorter than the diff path",
			"svc/add.go",
			map[string][]int{"internal/svc/add.go": {4, 8}, "other/add.go": {1}},
			"internal/svc/add.go",
		},
		{
			"the most specific suffix wins",
			"github.com/example/proj/internal/svc/add.go",
			map[string][]int{"add.go": {1}, "svc/add.go": {2}, "internal/svc/add.go": {3}},
			"internal/svc/add.go",
		},
		{"suffix must align on a separator", "internal/svc/xadd.go", map[string][]int{"add.go": {1}}, ""},
		{
			"unique basename as a last resort",
			"vendored/layout/add.go",
			map[string][]int{"internal/svc/add.go": {4, 8}, "internal/svc/sub.go": {1}},
			"internal/svc/add.go",
		},
		{"ambiguous basename matches nothing", "other/add.go", map[string][]int{"a/add.go": {1}, "b/add.go": {2}}, ""},
		{"no relation", "internal/unrelated/y.go", map[string][]int{"internal/svc/add.go": {4}}, ""},
		{"no changed files", "internal/svc/add.go", nil, ""},
		{"duplicate line numbers collapse", "a.go", map[string][]int{"a.go": {3, 3, 5}}, "a.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchFile(c.report, c.added)
			if c.wantKey == "" {
				if got != nil {
					t.Errorf("matchFile(%q) = %v, want no match", c.report, got)
				}
				return
			}
			want := map[int]struct{}{}
			for _, ln := range c.added[c.wantKey] {
				want[ln] = struct{}{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("matchFile(%q) = %v, want the lines of %q: %v", c.report, got, c.wantKey, want)
			}
		})
	}
}

// TestDiffScopedMSI_basenameFallback guards the score when the report path has
// no suffix relation to the diff path (a different module root): a unique
// basename still lines the two up and only added lines are scored.
func TestDiffScopedMSI_basenameFallback(t *testing.T) {
	r := &Report{Files: []FileMutations{
		{FileName: "vendored/layout/add.go", Mutations: []Mutation{
			{Line: 4, Status: "KILLED"},
			{Line: 8, Status: "LIVED"},
			{Line: 50, Status: "LIVED"}, // not an added line
		}},
	}}
	added := map[string][]int{"internal/svc/add.go": {4, 8}, "internal/svc/sub.go": {1}}
	if killedN, total := r.DiffScopedMSI(added); killedN != 1 || total != 2 {
		t.Fatalf("MSI = %d/%d, want 1/2", killedN, total)
	}
}

// TestDiffScopedMSI_ambiguousBasenameContributesNothing guards the conservative
// choice: when two changed files share the report file's basename, the match is
// a guess, so the report file is left out of the score.
func TestDiffScopedMSI_ambiguousBasenameContributesNothing(t *testing.T) {
	r := &Report{Files: []FileMutations{
		{FileName: "other/add.go", Mutations: []Mutation{{Line: 1, Status: "KILLED"}}},
	}}
	added := map[string][]int{"a/add.go": {1}, "b/add.go": {1}}
	if killedN, total := r.DiffScopedMSI(added); killedN != 0 || total != 0 {
		t.Fatalf("MSI = %d/%d, want 0/0", killedN, total)
	}
}

// TestDiffScopedMSI_counts guards the tally: every mutant on an added line is
// counted on its own, files add up, and degenerate inputs (no files, no
// mutants, no changed lines) score 0/0 rather than failing.
func TestDiffScopedMSI_counts(t *testing.T) {
	cases := []struct {
		name       string
		r          *Report
		added      map[string][]int
		wantKilled int
		wantTotal  int
	}{
		{
			"several mutants on one line count individually",
			&Report{Files: []FileMutations{{FileName: "a.go", Mutations: []Mutation{
				{Line: 3, Status: "KILLED"}, {Line: 3, Status: "KILLED"}, {Line: 3, Status: "LIVED"},
			}}}},
			map[string][]int{"a.go": {3}}, 2, 3,
		},
		{
			"files add up",
			&Report{Files: []FileMutations{
				{FileName: "a.go", Mutations: []Mutation{{Line: 1, Status: "KILLED"}}},
				{FileName: "pkg/b.go", Mutations: []Mutation{{Line: 2, Status: "LIVED"}, {Line: 3, Status: "TIMED OUT"}}},
			}},
			map[string][]int{"a.go": {1}, "pkg/b.go": {2, 3}}, 2, 3,
		},
		{"report without files", &Report{}, map[string][]int{"a.go": {1}}, 0, 0},
		{
			"file without mutants",
			&Report{Files: []FileMutations{{FileName: "a.go"}}},
			map[string][]int{"a.go": {1}}, 0, 0,
		},
		{
			"no changed files",
			&Report{Files: []FileMutations{{FileName: "a.go", Mutations: []Mutation{{Line: 1, Status: "KILLED"}}}}},
			nil, 0, 0,
		},
		{
			"matched file with no added lines",
			&Report{Files: []FileMutations{{FileName: "a.go", Mutations: []Mutation{{Line: 1, Status: "KILLED"}}}}},
			map[string][]int{"a.go": {}}, 0, 0,
		},
		{
			"statuses that say nothing about the tests are excluded",
			&Report{Files: []FileMutations{{FileName: "a.go", Mutations: []Mutation{
				{Line: 1, Status: "RUNNABLE"}, {Line: 1, Status: "NOT COVERED"}, {Line: 1, Status: ""},
			}}}},
			map[string][]int{"a.go": {1}}, 0, 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if killedN, total := c.r.DiffScopedMSI(c.added); killedN != c.wantKilled || total != c.wantTotal {
				t.Errorf("MSI = %d/%d, want %d/%d", killedN, total, c.wantKilled, c.wantTotal)
			}
		})
	}
}
