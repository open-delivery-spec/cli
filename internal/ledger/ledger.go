// Package ledger persists an append-only history of per-run pipeline results.
//
// The per-PR pipeline computes quality and debt signals — technical debt delta,
// test coverage, defect density, duplication — that git history cannot
// reconstruct on its own: they exist only for the moment a run happens and are
// otherwise thrown away with the workflow artifact. The ledger records one line
// per run so `ods report` can later show how those signals trend over time, and
// in particular whether AI-attributed changes carry more or fewer defects than
// human ones.
//
// The format is newline-delimited JSON (JSONL): one immutable Record per line,
// only ever appended. That makes it merge-friendly on a dedicated branch and
// trivially streamable — the same append-only shape brainfile uses for its
// coordination ledger.
package ledger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SchemaV1 identifies the version-1 record shape. It is written into every
// record so future readers can evolve the schema without misreading old lines.
const SchemaV1 = "ods.dev/ledger/v1"

// Record is a single pipeline run's captured signals — the unit the ledger
// appends. Fields mirror the detector and scorer outputs so a record can be
// built directly from a scored run with no recomputation.
type Record struct {
	Schema    string    `json:"schema"`
	Timestamp time.Time `json:"ts"`
	PR        int       `json:"pr,omitempty"`
	BaseSHA   string    `json:"base_sha,omitempty"`
	HeadSHA   string    `json:"head_sha,omitempty"`
	Branch    string    `json:"branch,omitempty"`

	// Attribution (from detect).
	AIGenerated  bool    `json:"ai_generated"`
	AIConfidence float64 `json:"ai_confidence"`
	AICodeRatio  float64 `json:"ai_code_ratio"`

	// Quality / debt (from score).
	Verdict            string  `json:"verdict"`
	TechnicalDebtDelta float64 `json:"technical_debt_delta"`
	DefectDensity      float64 `json:"defect_density"`
	CriticalIssues     int     `json:"critical_issues"`
	TestCoverage       float64 `json:"test_coverage"` // [0,1], or -1 when not measured
	TestCoverageSource string  `json:"test_coverage_source,omitempty"`
	DuplicationRate    float64 `json:"duplication_rate"`
}

// Append writes rec as one JSON line to the ledger at path, creating the file
// and any parent directories if they do not exist. The schema stamp and a
// timestamp are filled in when absent so callers need not set them.
func Append(path string, rec Record) error {
	if rec.Schema == "" {
		rec.Schema = SchemaV1
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshaling ledger record: %w", err)
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating ledger directory: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening ledger file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("writing ledger record: %w", err)
	}
	return nil
}

// Load reads all records from the ledger at path in file order. Blank lines are
// skipped, and a malformed line is tolerated (skipped) rather than aborting the
// whole read — an append-only log accumulated by many runs should stay readable
// even if one line was truncated. A missing file returns an empty slice and no
// error, so callers can treat "no ledger yet" as simply having no history.
func Load(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening ledger file: %w", err)
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // tolerate a corrupt/partial line
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return records, fmt.Errorf("reading ledger file: %w", err)
	}
	return records, nil
}
