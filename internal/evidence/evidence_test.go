package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/policy"
)

func fullInput() (*policy.EvalInput, *detector.DetectionResult, *policy.EvalResult, Meta) {
	in := &policy.EvalInput{
		AIGenerated:      true,
		AIConfidence:     0.95,
		DetectionSources: []string{"commit-trailer", "branch-name"},
		EvidenceTier:     "attested",
		PatchCoverage:    0.75,
		MutationScore:    0.5,
		ChangedFiles:     []string{"internal/svc/add.go", "docs/guide.md"},
		AIFiles: []policy.EvalFileInfo{
			{Path: "internal/svc/add.go", AILines: 40, TotalLines: 60, Confidence: 0.9},
		},
		MergeConfidence: &policy.EvalMergeConfidence{
			SourceFilesChanged: 1, TestFilesChanged: 1, TestsTouched: true,
		},
	}
	det := &detector.DetectionResult{
		AIGenerated: true, Confidence: 0.95,
		Sources: []string{"commit-trailer", "branch-name"},
		Evidence: []detector.Evidence{
			{Source: "commit-trailer", Signal: "AI-assisted commit abc1234 (tool: Claude)", Confidence: 0.9},
			{Source: "branch-name", Signal: "Branch 'claude/x' has AI-prefixed segment", Confidence: 0.35},
		},
	}
	res := &policy.EvalResult{Allowed: true, ReviewTier: "standard", Warnings: []string{"w"}}
	meta := Meta{
		Repo: "open-delivery-spec/cli", PR: "83",
		HeadSHA: "8f5a957c0e2b9d4f6a1e3c5b7d9f0a2c4e6b8d0f", DiffBase: "e6d332d",
		Branch:      "feature/evidence-tier",
		RunURL:      "https://github.com/open-delivery-spec/cli/actions/runs/31002889583",
		ToolVersion: "0.7.5", PipelineIntegrity: "ok",
		Timestamp: time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC),
	}
	return in, det, res, meta
}

// vendoredLoader serves the schema's absolute $ref URLs from testdata so the
// test never touches the network.
type vendoredLoader struct{ files map[string]string }

func (l vendoredLoader) Load(url string) (any, error) {
	path, ok := l.files[url]
	if !ok {
		return nil, fmt.Errorf("refusing remote fetch in tests: %s", url)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return jsonschema.UnmarshalJSON(f)
}

// TestBuild_ValidatesAgainstOfficialSchema is the load-bearing test: every
// document Build can produce must be a valid CycloneDX 1.6 BOM per the
// official schema (vendored in testdata).
func TestBuild_ValidatesAgainstOfficialSchema(t *testing.T) {
	vendored := vendoredLoader{files: map[string]string{
		"http://cyclonedx.org/schema/jsf-0.82.schema.json":  "testdata/jsf-0.82.schema.json",
		"https://cyclonedx.org/schema/jsf-0.82.schema.json": "testdata/jsf-0.82.schema.json",
		"http://cyclonedx.org/schema/spdx.schema.json":      "testdata/spdx.schema.json",
		"https://cyclonedx.org/schema/spdx.schema.json":     "testdata/spdx.schema.json",
		"http://cyclonedx.org/schema/bom-1.6.schema.json":   "testdata/bom-1.6.schema.json",
		"https://cyclonedx.org/schema/bom-1.6.schema.json":  "testdata/bom-1.6.schema.json",
	}}
	c := jsonschema.NewCompiler()
	c.UseLoader(jsonschema.SchemeURLLoader{
		"file":  jsonschema.FileLoader{},
		"http":  vendored,
		"https": vendored,
	})
	schema, err := c.Compile("testdata/bom-1.6.schema.json")
	if err != nil {
		t.Fatalf("compiling official schema: %v", err)
	}

	validate := func(t *testing.T, doc *Document) {
		t.Helper()
		raw, err := doc.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("re-parse: %v", err)
		}
		if err := schema.Validate(v); err != nil {
			t.Fatalf("document does not validate against CycloneDX 1.6:\n%v\n---\n%s", err, raw)
		}
	}

	t.Run("full AI change", func(t *testing.T) {
		validate(t, Build(fullInput()))
	})
	t.Run("human change, nothing measured", func(t *testing.T) {
		in := &policy.EvalInput{PatchCoverage: -1, MutationScore: -1, ChangedFiles: []string{"a.go"}}
		det := &detector.DetectionResult{}
		validate(t, Build(in, det, &policy.EvalResult{Allowed: true}, Meta{}))
	})
	t.Run("nil detector and result", func(t *testing.T) {
		in := &policy.EvalInput{PatchCoverage: -1, MutationScore: -1}
		validate(t, Build(in, nil, nil, Meta{}))
	})
}

func TestBuild_ContentInvariants(t *testing.T) {
	doc := Build(fullInput())

	// Conformance carries the measured value; confidence carries the tier.
	rows := map[string]AttestationMap{}
	for _, m := range doc.Declarations.Attestations[0].Map {
		rows[m.Requirement] = m
	}
	if got := rows["req:ods-r3"].Conformance.Score; got != 0.75 {
		t.Errorf("R3 conformance = %v, want 0.75 (the measured patch coverage)", got)
	}
	if got := rows["req:ods-r4"].Conformance.Score; got != 0.5 {
		t.Errorf("R4 conformance = %v, want 0.5 (the measured mutation score)", got)
	}
	if got := rows["req:ods-r1"].Confidence.Score; got != 0.9 {
		t.Errorf("R1 confidence = %v, want 0.9 (attested tier)", got)
	}
	if _, ok := rows["req:ods-r6"]; !ok {
		t.Error("expected an R6 row when PipelineIntegrity is reported")
	}

	// Every claim's evidence ref resolves, and every evidence carries the
	// re-fetchable run URL.
	evRefs := map[string]EvidenceRef{}
	for _, e := range doc.Declarations.Evidence {
		evRefs[e.BOMRef] = e
		if !strings.Contains(e.Description, "actions/runs/") {
			t.Errorf("evidence %s lacks a re-fetchable locator: %q", e.BOMRef, e.Description)
		}
	}
	for _, cl := range doc.Declarations.Claims {
		for _, ref := range cl.Evidence {
			if _, ok := evRefs[ref]; !ok {
				t.Errorf("claim %s references missing evidence %s", cl.BOMRef, ref)
			}
		}
	}

	// The affirmation states the not-forensic boundary.
	if !strings.Contains(doc.Declarations.Affirmation.Statement, "not forensic proof") {
		t.Error("affirmation must state the not-forensic boundary")
	}

	// All changed files appear as components (the denominator), AI file carries
	// identity evidence.
	if len(doc.Components) != 2 {
		t.Fatalf("components = %d, want 2 (all changed files)", len(doc.Components))
	}
	var aiComp *Component
	for i := range doc.Components {
		if doc.Components[i].Name == "internal/svc/add.go" {
			aiComp = &doc.Components[i]
		}
	}
	if aiComp == nil || aiComp.Evidence == nil || len(aiComp.Evidence.Identity) == 0 {
		t.Fatal("AI-attributed file must carry identity evidence")
	}
	if aiComp.Evidence.Identity[0].Methods[0].Technique != "attestation" {
		t.Errorf("trailer method technique = %q, want attestation", aiComp.Evidence.Identity[0].Methods[0].Technique)
	}
}

func TestBuild_OmitsUnmeasuredRows(t *testing.T) {
	in := &policy.EvalInput{PatchCoverage: -1, MutationScore: -1}
	doc := Build(in, &detector.DetectionResult{}, &policy.EvalResult{Allowed: true}, Meta{})
	for _, m := range doc.Declarations.Attestations[0].Map {
		if m.Requirement == "req:ods-r4" {
			t.Error("R4 must be omitted when mutation is not measured")
		}
		if m.Requirement == "req:ods-r6" {
			t.Error("R6 must be omitted when pipeline integrity is unknown")
		}
	}
}
