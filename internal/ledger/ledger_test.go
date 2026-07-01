package ledger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendCreatesFileAndDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "ledger.jsonl")

	if err := Append(path, Record{PR: 1, Verdict: "decrease"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("ledger file not created: %v", err)
	}

	recs, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].PR != 1 || recs[0].Verdict != "decrease" {
		t.Fatalf("unexpected record: %+v", recs[0])
	}
}

func TestAppendStampsSchemaAndTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")

	if err := Append(path, Record{PR: 7}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	recs, _ := Load(path)
	if recs[0].Schema != SchemaV1 {
		t.Errorf("schema = %q, want %q", recs[0].Schema, SchemaV1)
	}
	if recs[0].Timestamp.IsZero() {
		t.Error("timestamp not stamped")
	}
}

func TestAppendPreservesCallerSchemaAndTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	if err := Append(path, Record{Schema: "custom/v9", Timestamp: ts}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	recs, _ := Load(path)
	if recs[0].Schema != "custom/v9" {
		t.Errorf("schema overwritten: %q", recs[0].Schema)
	}
	if !recs[0].Timestamp.Equal(ts) {
		t.Errorf("timestamp overwritten: %v", recs[0].Timestamp)
	}
}

func TestAppendIsAdditive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")

	for i := 1; i <= 3; i++ {
		if err := Append(path, Record{PR: i}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	recs, _ := Load(path)
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	for i, r := range recs {
		if r.PR != i+1 {
			t.Errorf("record %d: PR = %d, want %d", i, r.PR, i+1)
		}
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	recs, err := Load(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if err != nil {
		t.Fatalf("Load of missing file errored: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("got %d records, want 0", len(recs))
	}
}

func TestLoadSkipsBlankAndCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	content := `{"pr":1,"verdict":"decrease"}

not json at all
{"pr":2,"verdict":"increase"}
{truncated
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 (blank + corrupt lines skipped)", len(recs))
	}
	if recs[0].PR != 1 || recs[1].PR != 2 {
		t.Fatalf("unexpected records: %+v", recs)
	}
}

func TestRoundTripPreservesFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	in := Record{
		PR:                 42,
		BaseSHA:            "aaaa",
		HeadSHA:            "bbbb",
		Branch:             "feature/x",
		AIGenerated:        true,
		AIConfidence:       0.9,
		AICodeRatio:        0.42,
		Verdict:            "increase",
		TechnicalDebtDelta: 3.5,
		DefectDensity:      0.8,
		CriticalIssues:     2,
		TestCoverage:       0.71,
		TestCoverageSource: "go",
		DuplicationRate:    0.05,
	}
	if err := Append(path, in); err != nil {
		t.Fatalf("Append: %v", err)
	}
	recs, _ := Load(path)
	got := recs[0]
	got.Schema = ""
	got.Timestamp = time.Time{}
	if got != in {
		t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", got, in)
	}
}
