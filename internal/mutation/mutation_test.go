package mutation

import (
	"os"
	"path/filepath"
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
