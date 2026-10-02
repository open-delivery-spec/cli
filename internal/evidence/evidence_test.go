package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
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
	// Every row in its failing state, every detection source (and so every
	// identity-evidence technique), the commit-based change name, and the
	// merge-confidence fallback for the test-adequacy row.
	t.Run("heuristic-only AI with every row failing", func(t *testing.T) {
		in := &policy.EvalInput{
			AIGenerated: true, EvidenceTier: "inferred", PatchCoverage: -1, MutationScore: 0,
			ChangedFiles: []string{"a.go"},
			AIFiles:      []policy.EvalFileInfo{{Path: "a.go", AILines: 3, TotalLines: 9, Confidence: 0.3}},
			MergeConfidence: &policy.EvalMergeConfidence{
				SourceFilesChanged: 1, AddedSourceWithoutTests: true,
			},
		}
		det := &detector.DetectionResult{
			AIGenerated: true, Confidence: 0.3,
			Sources: []string{"diff-heuristics", "git-ai-notes", "pr-body", "a-future-source"},
			Evidence: []detector.Evidence{
				{Source: "diff-heuristics", Signal: "uniform style", Confidence: 0.3},
				{Source: "git-ai-notes", Signal: "3 lines attributed", Confidence: 1},
				{Source: "pr-body", Signal: "AI-assisted: yes", Confidence: 0.7},
				{Source: "a-future-source", Signal: "?", Confidence: 0.1},
			},
		}
		res := &policy.EvalResult{Allowed: false, Denials: []string{"blocked"}}
		meta := Meta{Repo: "o/r", HeadSHA: "8f5a957c0e2b", PipelineIntegrity: "inconclusive"}
		validate(t, Build(in, det, res, meta))
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

// ---- helpers for the focused Build tests ----

// minimalInput is an input with nothing measured: neither patch coverage nor a
// mutation score, and no files.
func minimalInput() *policy.EvalInput {
	return &policy.EvalInput{PatchCoverage: -1, MutationScore: -1}
}

// attestationRows indexes a document's attestation rows by requirement ref.
func attestationRows(doc *Document) map[string]AttestationMap {
	rows := map[string]AttestationMap{}
	for _, m := range doc.Declarations.Attestations[0].Map {
		rows[m.Requirement] = m
	}
	return rows
}

// predicateOf returns the predicate of the claim a requirement row points at.
func predicateOf(t *testing.T, doc *Document, row AttestationMap) string {
	t.Helper()
	for _, ref := range row.Claims {
		for _, c := range doc.Declarations.Claims {
			if c.BOMRef == ref {
				return c.Predicate
			}
		}
	}
	t.Fatalf("row %s references claims %v that do not exist", row.Requirement, row.Claims)
	return ""
}

// ---- tier and technique mappings ----

// TestTierConfidence guards the tier-to-confidence mapping used on attribution
// rows: each known tier has its own score and rationale, and an empty or
// unrecognized tier is the neutral "no attribution signal" answer.
func TestTierConfidence(t *testing.T) {
	cases := []struct {
		tier      string
		wantScore float64
		wantWhy   string
	}{
		{"corroborated", 1.0, "Evidence tier: corroborated"},
		{"attested", 0.9, "Evidence tier: attested"},
		{"inferred", 0.35, "Evidence tier: inferred"},
		{"", 0.5, "No attribution signal present"},
		{"a-tier-from-the-future", 0.5, "No attribution signal present"},
	}
	for _, c := range cases {
		t.Run(c.tier, func(t *testing.T) {
			score, why := tierConfidence(c.tier)
			if score != c.wantScore {
				t.Errorf("score = %v, want %v", score, c.wantScore)
			}
			if !strings.HasPrefix(why, c.wantWhy) {
				t.Errorf("rationale = %q, want it to start with %q", why, c.wantWhy)
			}
		})
	}
}

// TestTechniqueFor guards the mapping from a detection source to the CycloneDX
// identity-evidence technique: declared sources are attestations, git-ai
// notes are measured from the code, the branch name is a filename-style signal,
// and anything else (diff heuristics, sources added later) is "other".
func TestTechniqueFor(t *testing.T) {
	cases := map[string]string{
		"commit-trailer":  "attestation",
		"pr-body":         "attestation",
		"git-ai-notes":    "source-code-analysis",
		"branch-name":     "filename",
		"diff-heuristics": "other",
		"a-future-source": "other",
		"":                "other",
	}
	for source, want := range cases {
		if got := techniqueFor(source); got != want {
			t.Errorf("techniqueFor(%q) = %q, want %q", source, got, want)
		}
	}
}

// TestShortSHA guards the abbreviation used in the change name: seven
// characters, with shorter values left intact.
func TestShortSHA(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"abc":               "abc",
		"abcdef0":           "abcdef0",
		"abcdef01":          "abcdef0",
		"8f5a957c0e2b9d4f6": "8f5a957",
	}
	for in, want := range cases {
		if got := shortSHA(in); got != want {
			t.Errorf("shortSHA(%q) = %q, want %q", in, got, want)
		}
	}
}

// uuidV4 matches an RFC 4122 version-4 UUID: version nibble 4, variant 10xx.
var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestNewUUID guards the serial number format: each call yields a well-formed
// version-4 UUID, and successive calls differ.
func TestNewUUID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := newUUID()
		if !uuidV4.MatchString(id) {
			t.Fatalf("newUUID() = %q, want an RFC 4122 v4 UUID", id)
		}
		if seen[id] {
			t.Fatalf("newUUID() repeated %q", id)
		}
		seen[id] = true
	}
}

// TestBuild_SerialNumber guards that the document's serial number is a fresh
// urn:uuid every time, as CycloneDX asks.
func TestBuild_SerialNumber(t *testing.T) {
	a := Build(minimalInput(), nil, nil, Meta{})
	b := Build(minimalInput(), nil, nil, Meta{})
	for _, doc := range []*Document{a, b} {
		id, ok := strings.CutPrefix(doc.SerialNumber, "urn:uuid:")
		if !ok || !uuidV4.MatchString(id) {
			t.Errorf("SerialNumber = %q, want urn:uuid:<v4 uuid>", doc.SerialNumber)
		}
	}
	if a.SerialNumber == b.SerialNumber {
		t.Errorf("two documents share serial number %q", a.SerialNumber)
	}
}

// ---- metadata ----

// TestBuild_ChangeName guards how the change is named in the document: just
// "change" without a repository, otherwise the repository followed by the PR
// number, or by the abbreviated head commit when there is no PR. The same name
// appears on the metadata component, the declaration target and the summary.
func TestBuild_ChangeName(t *testing.T) {
	cases := []struct {
		name string
		meta Meta
		want string
	}{
		{"no repository", Meta{PR: "7", HeadSHA: "8f5a957c0e2b"}, "change"},
		{"repository only", Meta{Repo: "o/r"}, "o/r"},
		{"the PR number wins over the commit", Meta{Repo: "o/r", PR: "7", HeadSHA: "8f5a957c0e2b"}, "o/r PR #7"},
		{"long commit is abbreviated", Meta{Repo: "o/r", HeadSHA: "8f5a957c0e2b9d4f6a1e"}, "o/r@8f5a957"},
		{"short commit is kept whole", Meta{Repo: "o/r", HeadSHA: "abc12"}, "o/r@abc12"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := Build(minimalInput(), nil, nil, c.meta)
			if got := doc.Metadata.Component.Name; got != c.want {
				t.Errorf("metadata component name = %q, want %q", got, c.want)
			}
			if got := doc.Declarations.Targets.Components[0].Name; got != c.want {
				t.Errorf("target name = %q, want %q", got, c.want)
			}
			wantSummary := "ODS self-assessment of " + c.want + " against ODS AI-Assisted Code Governance v1"
			if got := doc.Declarations.Attestations[0].Summary; got != wantSummary {
				t.Errorf("summary = %q, want %q", got, wantSummary)
			}
		})
	}
}

// TestBuild_Timestamp guards the document time: an explicit timestamp is used
// as given, and the zero value means "now" in UTC. The same instant stamps
// every piece of evidence.
func TestBuild_Timestamp(t *testing.T) {
	t.Run("explicit", func(t *testing.T) {
		_, _, _, meta := fullInput()
		doc := Build(minimalInput(), nil, nil, meta)
		if got := doc.Metadata.Timestamp; got != "2026-08-06T14:00:00Z" {
			t.Errorf("Timestamp = %q, want 2026-08-06T14:00:00Z", got)
		}
		for _, e := range doc.Declarations.Evidence {
			if e.Created != doc.Metadata.Timestamp {
				t.Errorf("evidence %s created at %q, want %q", e.BOMRef, e.Created, doc.Metadata.Timestamp)
			}
		}
	})

	t.Run("zero value means now in UTC", func(t *testing.T) {
		before := time.Now().UTC().Truncate(time.Second)
		doc := Build(minimalInput(), nil, nil, Meta{})
		after := time.Now().UTC()

		if !strings.HasSuffix(doc.Metadata.Timestamp, "Z") {
			t.Errorf("Timestamp = %q, want a UTC (Z) time", doc.Metadata.Timestamp)
		}
		got, err := time.Parse(time.RFC3339, doc.Metadata.Timestamp)
		if err != nil {
			t.Fatalf("Timestamp %q is not RFC 3339: %v", doc.Metadata.Timestamp, err)
		}
		if got.Before(before) || got.After(after) {
			t.Errorf("Timestamp = %v, want it between %v and %v", got, before, after)
		}
		for _, e := range doc.Declarations.Evidence {
			if e.Created != doc.Metadata.Timestamp {
				t.Errorf("evidence %s created at %q, want %q", e.BOMRef, e.Created, doc.Metadata.Timestamp)
			}
		}
	})
}

// TestBuild_ToolAndChangeProperties guards the identifiers recorded on the
// change: the producing tool and its version, and one ods:* property per
// non-empty locator, in a fixed order, with empty ones omitted.
func TestBuild_ToolAndChangeProperties(t *testing.T) {
	cases := []struct {
		name string
		meta Meta
		want []Property
	}{
		{"nothing known", Meta{}, nil},
		{
			"only some known",
			Meta{PR: "5", Branch: "feature/x"},
			[]Property{{"ods:pr", "5"}, {"ods:branch", "feature/x"}},
		},
		{
			"everything, in fixed order",
			Meta{Branch: "b", RunURL: "https://ci/run/1", DiffBase: "main", HeadSHA: "abc", PR: "5"},
			[]Property{
				{"ods:pr", "5"}, {"ods:head_sha", "abc"}, {"ods:diff_base", "main"},
				{"ods:branch", "b"}, {"ods:workflow_run", "https://ci/run/1"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.meta.ToolVersion = "9.9.9"
			doc := Build(minimalInput(), nil, nil, c.meta)

			tools := doc.Metadata.Tools.Components
			if len(tools) != 1 || tools[0].Name != "ods" || tools[0].Version != "9.9.9" {
				t.Errorf("tools = %+v, want the single tool ods 9.9.9", tools)
			}
			got := doc.Metadata.Component.Properties
			if len(c.want) == 0 {
				if len(got) != 0 {
					t.Errorf("properties = %v, want none", got)
				}
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("properties = %v, want %v", got, c.want)
			}
		})
	}
}

// ---- components ----

// TestBuild_FileComponents guards the component list: every changed file is a
// component, in diff order (auditors want the denominator); only AI-attributed
// files carry identity evidence, built from the detector's signals, plus the
// line counts and the evidence tier; AI files outside the diff are not listed.
func TestBuild_FileComponents(t *testing.T) {
	in := &policy.EvalInput{
		PatchCoverage: -1, MutationScore: -1,
		EvidenceTier: "corroborated",
		ChangedFiles: []string{"b.go", "a.go", "docs/readme.md"},
		AIFiles: []policy.EvalFileInfo{
			{Path: "a.go", AILines: 12, TotalLines: 30, Confidence: 0.8},
			{Path: "not/in/the/diff.go", AILines: 5, TotalLines: 5, Confidence: 1},
		},
	}
	det := &detector.DetectionResult{Evidence: []detector.Evidence{
		{Source: "git-ai-notes", Signal: "12 lines attributed", Confidence: 1.0},
		{Source: "diff-heuristics", Signal: "uniform style", Confidence: 0.3},
		{Source: "pr-body", Signal: "AI-assisted: yes", Confidence: 0.7},
	}}
	doc := Build(in, det, nil, Meta{})

	var names []string
	for _, c := range doc.Components {
		names = append(names, c.Name)
		if c.Type != "file" || c.BOMRef != "file:"+c.Name {
			t.Errorf("component %+v, want type file with bom-ref file:%s", c, c.Name)
		}
	}
	if want := []string{"b.go", "a.go", "docs/readme.md"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("components = %v, want %v (the changed files, in order)", names, want)
	}

	for _, c := range []Component{doc.Components[0], doc.Components[2]} {
		if c.Evidence != nil || len(c.Properties) != 0 {
			t.Errorf("%s is not AI-attributed but carries evidence %+v / properties %v", c.Name, c.Evidence, c.Properties)
		}
	}

	ai := doc.Components[1]
	if ai.Evidence == nil || len(ai.Evidence.Identity) != 1 {
		t.Fatalf("a.go identity evidence = %+v, want exactly one identity", ai.Evidence)
	}
	id := ai.Evidence.Identity[0]
	if id.Field != "name" || id.ConcludedValue != "ai-assisted" || id.Confidence != 0.8 {
		t.Errorf("identity = %+v, want field name, concluded ai-assisted, confidence 0.8", id)
	}
	wantMethods := []Method{
		{Technique: "source-code-analysis", Confidence: 1.0, Value: "git-ai-notes: 12 lines attributed"},
		{Technique: "other", Confidence: 0.3, Value: "diff-heuristics: uniform style"},
		{Technique: "attestation", Confidence: 0.7, Value: "pr-body: AI-assisted: yes"},
	}
	if !reflect.DeepEqual(id.Methods, wantMethods) {
		t.Errorf("methods = %+v, want %+v", id.Methods, wantMethods)
	}
	wantProps := []Property{
		{"ods:ai_lines", "12"}, {"ods:total_lines", "30"}, {"ods:evidence_tier", "corroborated"},
	}
	if !reflect.DeepEqual(ai.Properties, wantProps) {
		t.Errorf("properties = %v, want %v", ai.Properties, wantProps)
	}
}

// TestBuild_FileComponentsWithoutTierOrDetector guards the optional parts of an
// AI file's component: with no evidence tier there is no ods:evidence_tier
// property, and with no detection result the identity has no methods.
func TestBuild_FileComponentsWithoutTierOrDetector(t *testing.T) {
	in := &policy.EvalInput{
		PatchCoverage: -1, MutationScore: -1,
		ChangedFiles: []string{"a.go"},
		AIFiles:      []policy.EvalFileInfo{{Path: "a.go", AILines: 1, TotalLines: 2, Confidence: 0.5}},
	}
	doc := Build(in, nil, nil, Meta{})
	comp := doc.Components[0]
	if len(comp.Evidence.Identity) != 1 || len(comp.Evidence.Identity[0].Methods) != 0 {
		t.Errorf("identity = %+v, want one identity without methods", comp.Evidence.Identity)
	}
	for _, p := range comp.Properties {
		if p.Name == "ods:evidence_tier" {
			t.Errorf("unexpected %s property with no evidence tier", p.Name)
		}
	}
	if len(comp.Properties) != 2 {
		t.Errorf("properties = %v, want only the line counts", comp.Properties)
	}
}

// ---- requirement rows ----

// TestBuild_DisclosureRow guards ODS-R1: conformance says whether attribution
// is disclosed, and confidence says how strong the evidence tier is, never one
// folded into the other. AI that is only suspected (heuristic tier, or no tier)
// is non-conformant; no detected AI is vacuously conformant.
func TestBuild_DisclosureRow(t *testing.T) {
	aiDetection := &detector.DetectionResult{AIGenerated: true, Confidence: 0.8, Sources: []string{"x"}}
	cases := []struct {
		name            string
		tier            string
		det             *detector.DetectionResult
		wantConformance float64
		wantConfidence  float64
		wantPredicate   string
	}{
		{"attested AI", "attested", aiDetection, 1.0, 0.9, "disclosed via author/tool attribution"},
		{"corroborated AI", "corroborated", aiDetection, 1.0, 1.0, "disclosed via author/tool attribution"},
		{"AI suspected from heuristics", "inferred", aiDetection, 0.0, 0.35, "suspected from heuristics only"},
		{"AI with no tier", "", aiDetection, 0.0, 0.5, "suspected from heuristics only"},
		{"no AI detected", "", &detector.DetectionResult{}, 1.0, 0.5, "No AI assistance was detected"},
		{"no detection result", "attested", nil, 1.0, 0.5, "No AI assistance was detected"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := minimalInput()
			in.EvidenceTier = c.tier
			doc := Build(in, c.det, nil, Meta{})
			row, ok := attestationRows(doc)["req:ods-r1"]
			if !ok {
				t.Fatal("no R1 row")
			}
			if row.Conformance.Score != c.wantConformance {
				t.Errorf("conformance = %v, want %v", row.Conformance.Score, c.wantConformance)
			}
			if row.Confidence.Score != c.wantConfidence {
				t.Errorf("confidence = %v, want %v", row.Confidence.Score, c.wantConfidence)
			}
			if p := predicateOf(t, doc, row); !strings.Contains(p, c.wantPredicate) {
				t.Errorf("predicate = %q, want it to contain %q", p, c.wantPredicate)
			}
		})
	}
}

// TestBuild_GradedRow guards ODS-R2: it is always present and names the tier,
// calling an absent tier "inconclusive".
func TestBuild_GradedRow(t *testing.T) {
	for tier, want := range map[string]string{"attested": `"attested"`, "": `"inconclusive"`} {
		in := minimalInput()
		in.EvidenceTier = tier
		doc := Build(in, nil, nil, Meta{})
		row, ok := attestationRows(doc)["req:ods-r2"]
		if !ok {
			t.Fatalf("tier %q: no R2 row", tier)
		}
		if p := predicateOf(t, doc, row); !strings.Contains(p, want) {
			t.Errorf("tier %q: predicate = %q, want it to contain %s", tier, p, want)
		}
	}
}

// TestBuild_TestAdequacyRow guards ODS-R3: measured patch coverage is the
// preferred basis (even 0%, which is measured, not missing); without it the
// row falls back to whether source changes came with test changes; and a change
// with neither basis (nothing tracked, or no source files) has no row.
func TestBuild_TestAdequacyRow(t *testing.T) {
	cases := []struct {
		name          string
		patch         float64
		mc            *policy.EvalMergeConfidence
		wantRow       bool
		wantScore     float64
		wantPredicate string
	}{
		{"patch coverage", 0.5, nil, true, 0.5, "50% of the diff's added lines are executed"},
		{"zero patch coverage is still measured", 0, nil, true, 0, "0% of the diff's added lines are executed"},
		{
			"patch coverage wins over merge confidence", 0.8,
			&policy.EvalMergeConfidence{SourceFilesChanged: 1, AddedSourceWithoutTests: true},
			true, 0.8, "80% of the diff's added lines are executed",
		},
		{
			"fallback: source changed with tests", -1,
			&policy.EvalMergeConfidence{SourceFilesChanged: 2, TestsTouched: true},
			true, 1.0, "Source changes are accompanied by test changes",
		},
		{
			"fallback: source added without tests", -1,
			&policy.EvalMergeConfidence{SourceFilesChanged: 1, AddedSourceWithoutTests: true},
			true, 0.0, "Source code was added without any test file",
		},
		{"no coverage and no merge confidence", -1, nil, false, 0, ""},
		{"no coverage and no source files", -1, &policy.EvalMergeConfidence{TestFilesChanged: 1}, false, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := minimalInput()
			in.PatchCoverage = c.patch
			in.MergeConfidence = c.mc
			doc := Build(in, nil, nil, Meta{})
			row, ok := attestationRows(doc)["req:ods-r3"]
			if ok != c.wantRow {
				t.Fatalf("R3 row present = %v, want %v", ok, c.wantRow)
			}
			if !ok {
				return
			}
			if row.Conformance.Score != c.wantScore {
				t.Errorf("conformance = %v, want %v", row.Conformance.Score, c.wantScore)
			}
			if row.Confidence.Score != 1.0 {
				t.Errorf("confidence = %v, want 1.0 (corroborated from the run)", row.Confidence.Score)
			}
			if p := predicateOf(t, doc, row); !strings.Contains(p, c.wantPredicate) {
				t.Errorf("predicate = %q, want it to contain %q", p, c.wantPredicate)
			}
		})
	}
}

// TestBuild_MutationRow guards ODS-R4: present when a score was measured (0
// included), carrying that score; absent when it was not (-1).
func TestBuild_MutationRow(t *testing.T) {
	cases := []struct {
		score     float64
		wantRow   bool
		wantInTxt string
	}{
		{-1, false, ""},
		{0, true, "0% of mutants"},
		{0.75, true, "75% of mutants"},
	}
	for _, c := range cases {
		in := minimalInput()
		in.MutationScore = c.score
		doc := Build(in, nil, nil, Meta{})
		row, ok := attestationRows(doc)["req:ods-r4"]
		if ok != c.wantRow {
			t.Fatalf("score %v: R4 row present = %v, want %v", c.score, ok, c.wantRow)
		}
		if !ok {
			continue
		}
		if row.Conformance.Score != c.score {
			t.Errorf("score %v: conformance = %v", c.score, row.Conformance.Score)
		}
		if p := predicateOf(t, doc, row); !strings.Contains(p, c.wantInTxt) {
			t.Errorf("score %v: predicate = %q, want it to contain %q", c.score, p, c.wantInTxt)
		}
	}
}

// TestBuild_PolicyRow guards ODS-R5: present only when a policy result exists,
// scoring the verdict and summarizing it (tier, warning and denial counts), with
// an unset review tier reported as the standard tier.
func TestBuild_PolicyRow(t *testing.T) {
	cases := []struct {
		name          string
		res           *policy.EvalResult
		wantRow       bool
		wantScore     float64
		wantPredicate string
	}{
		{"no policy result", nil, false, 0, ""},
		{
			"allowed with the default tier",
			&policy.EvalResult{Allowed: true},
			true, 1.0, "allowed=true, review_tier=standard, warnings=0, denials=0",
		},
		{
			"denied, elevated, with findings",
			&policy.EvalResult{
				Allowed: false, ReviewTier: policy.ReviewTierElevated,
				Warnings: []string{"w"}, Denials: []string{"d1", "d2"},
			},
			true, 0.0, "allowed=false, review_tier=elevated, warnings=1, denials=2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := Build(minimalInput(), nil, c.res, Meta{})
			row, ok := attestationRows(doc)["req:ods-r5"]
			if ok != c.wantRow {
				t.Fatalf("R5 row present = %v, want %v", ok, c.wantRow)
			}
			if !ok {
				return
			}
			if row.Conformance.Score != c.wantScore {
				t.Errorf("conformance = %v, want %v", row.Conformance.Score, c.wantScore)
			}
			if p := predicateOf(t, doc, row); !strings.Contains(p, c.wantPredicate) {
				t.Errorf("predicate = %q, want it to contain %q", p, c.wantPredicate)
			}
		})
	}
}

// TestBuild_PipelineIntegrityRow guards ODS-R6: only a reported status produces
// a row, "ok" conforms, and any other reported status does not.
func TestBuild_PipelineIntegrityRow(t *testing.T) {
	cases := []struct {
		status    string
		wantRow   bool
		wantScore float64
	}{
		{"", false, 0},
		{"ok", true, 1.0},
		{"inconclusive", true, 0.0},
	}
	for _, c := range cases {
		t.Run("status="+c.status, func(t *testing.T) {
			doc := Build(minimalInput(), nil, nil, Meta{PipelineIntegrity: c.status})
			row, ok := attestationRows(doc)["req:ods-r6"]
			if ok != c.wantRow {
				t.Fatalf("R6 row present = %v, want %v", ok, c.wantRow)
			}
			if !ok {
				return
			}
			if row.Conformance.Score != c.wantScore {
				t.Errorf("conformance = %v, want %v", row.Conformance.Score, c.wantScore)
			}
			if p := predicateOf(t, doc, row); !strings.Contains(p, c.status) {
				t.Errorf("predicate = %q, want it to mention the status %q", p, c.status)
			}
		})
	}
}

// TestBuild_EvidenceLocator guards the re-fetchable locator: the CI run URL is
// appended to every evidence description when known, and nothing is invented
// when it is not.
func TestBuild_EvidenceLocator(t *testing.T) {
	const url = "https://ci.example/runs/42"
	with := Build(minimalInput(), nil, &policy.EvalResult{Allowed: true}, Meta{RunURL: url})
	without := Build(minimalInput(), nil, &policy.EvalResult{Allowed: true}, Meta{})

	for _, e := range with.Declarations.Evidence {
		if !strings.HasSuffix(e.Description, ". Re-fetchable: "+url) {
			t.Errorf("evidence %s description = %q, want it to end with the run URL", e.BOMRef, e.Description)
		}
	}
	for _, e := range without.Declarations.Evidence {
		if strings.Contains(e.Description, "Re-fetchable") {
			t.Errorf("evidence %s description = %q, want no locator without a run URL", e.BOMRef, e.Description)
		}
	}
	if len(with.Declarations.Evidence) == 0 {
		t.Fatal("expected evidence entries")
	}
}
