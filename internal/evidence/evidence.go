// Package evidence serializes the facts the ODS pipeline computed for a change
// into an AI-code evidence document: a valid CycloneDX 1.6 BOM whose CDXA
// declarations carry per-requirement claims backed by re-fetchable evidence.
// See spec: docs/proposals/001-ai-code-evidence.md.
//
// Two invariants from the proposal:
//   - conformance carries the measured value, confidence carries how the fact
//     was obtained (the evidence tier) — the two are never merged;
//   - the document states its own limits: attribution is volunteered, not
//     forensic, and nothing here asserts code correctness.
package evidence

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/policy"
)

// Meta carries change identifiers and locators an auditor can re-fetch.
// Empty fields are omitted from the document.
type Meta struct {
	Repo        string // e.g. open-delivery-spec/cli (GITHUB_REPOSITORY)
	PR          string // PR number when known
	HeadSHA     string
	DiffBase    string
	Branch      string
	RunURL      string // CI workflow run URL — the primary re-fetchable locator
	ToolVersion string // ods version producing the document
	// PipelineIntegrity is "ok" or "inconclusive" when the CI wrapper knows
	// stage status (ODS_PIPELINE_INTEGRITY); empty when unknown, which omits
	// the pipeline-integrity requirement row rather than guessing.
	PipelineIntegrity string
	Timestamp         time.Time // zero value = time.Now().UTC()
}

// ---- CycloneDX 1.6 subset (only the fields ODS emits) ----

type Document struct {
	BOMFormat    string        `json:"bomFormat"`
	SpecVersion  string        `json:"specVersion"`
	SerialNumber string        `json:"serialNumber,omitempty"`
	Version      int           `json:"version"`
	Metadata     *Metadata     `json:"metadata,omitempty"`
	Components   []Component   `json:"components,omitempty"`
	Definitions  *Definitions  `json:"definitions,omitempty"`
	Declarations *Declarations `json:"declarations,omitempty"`
}

type Metadata struct {
	Timestamp string     `json:"timestamp,omitempty"`
	Tools     *Tools     `json:"tools,omitempty"`
	Component *Component `json:"component,omitempty"`
}

type Tools struct {
	Components []Component `json:"components,omitempty"`
}

type Component struct {
	BOMRef      string             `json:"bom-ref,omitempty"`
	Type        string             `json:"type"`
	Name        string             `json:"name"`
	Version     string             `json:"version,omitempty"`
	Description string             `json:"description,omitempty"`
	Evidence    *ComponentEvidence `json:"evidence,omitempty"`
	Properties  []Property         `json:"properties,omitempty"`
}

type ComponentEvidence struct {
	Identity []Identity `json:"identity,omitempty"`
}

type Identity struct {
	Field          string   `json:"field"`
	Confidence     float64  `json:"confidence,omitempty"`
	ConcludedValue string   `json:"concludedValue,omitempty"`
	Methods        []Method `json:"methods,omitempty"`
}

type Method struct {
	Technique  string  `json:"technique"`
	Confidence float64 `json:"confidence"`
	Value      string  `json:"value,omitempty"`
}

type Property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Definitions struct {
	Standards []Standard `json:"standards,omitempty"`
}

type Standard struct {
	BOMRef       string        `json:"bom-ref,omitempty"`
	Name         string        `json:"name"`
	Version      string        `json:"version,omitempty"`
	Description  string        `json:"description,omitempty"`
	Owner        string        `json:"owner,omitempty"`
	Requirements []Requirement `json:"requirements,omitempty"`
}

type Requirement struct {
	BOMRef     string `json:"bom-ref,omitempty"`
	Identifier string `json:"identifier"`
	Title      string `json:"title,omitempty"`
	Text       string `json:"text,omitempty"`
}

type Declarations struct {
	Targets      *Targets      `json:"targets,omitempty"`
	Evidence     []EvidenceRef `json:"evidence,omitempty"`
	Claims       []Claim       `json:"claims,omitempty"`
	Attestations []Attestation `json:"attestations,omitempty"`
	Affirmation  *Affirmation  `json:"affirmation,omitempty"`
}

type Targets struct {
	Components []Component `json:"components,omitempty"`
}

type EvidenceRef struct {
	BOMRef       string `json:"bom-ref,omitempty"`
	PropertyName string `json:"propertyName,omitempty"`
	Description  string `json:"description,omitempty"`
	Created      string `json:"created,omitempty"`
}

type Claim struct {
	BOMRef    string   `json:"bom-ref,omitempty"`
	Target    string   `json:"target,omitempty"`
	Predicate string   `json:"predicate,omitempty"`
	Evidence  []string `json:"evidence,omitempty"`
}

type Attestation struct {
	Summary string           `json:"summary,omitempty"`
	Map     []AttestationMap `json:"map,omitempty"`
}

type AttestationMap struct {
	Requirement string   `json:"requirement,omitempty"`
	Claims      []string `json:"claims,omitempty"`
	Conformance *Scored  `json:"conformance,omitempty"`
	Confidence  *Scored  `json:"confidence,omitempty"`
}

type Scored struct {
	Score     float64 `json:"score,omitempty"`
	Rationale string  `json:"rationale,omitempty"`
}

type Affirmation struct {
	Statement string `json:"statement,omitempty"`
}

// affirmationStatement is the document's own honesty boundary.
const affirmationStatement = "This document records deterministic, artifact-level evidence about AI-assisted code in the referenced change. Attribution reflects signals the tools and authors volunteered (graded by evidence tier); it is not forensic proof of authorship. Signals are advisory by default and never assert code correctness."

// tierConfidence maps an ODS evidence tier to the confidence score used in
// attestation rows about attribution-derived facts.
func tierConfidence(tier string) (float64, string) {
	switch tier {
	case "corroborated":
		return 1.0, "Evidence tier: corroborated (independently measured, e.g. git-ai line attribution)."
	case "attested":
		return 0.9, "Evidence tier: attested (declared by the author/tool; not independently measured)."
	case "inferred":
		return 0.35, "Evidence tier: inferred (heuristic only)."
	default:
		return 0.5, "No attribution signal present; absence of a volunteered signal is not proof of absence."
	}
}

// techniqueFor maps an ODS detection source to the CycloneDX identity-evidence
// technique that best describes how the signal was obtained.
func techniqueFor(source string) string {
	switch source {
	case "commit-trailer", "pr-body":
		return "attestation"
	case "git-ai-notes":
		return "source-code-analysis"
	case "branch-name":
		return "filename"
	default: // diff-heuristics and future sources
		return "other"
	}
}

// Build assembles the evidence document from the same inputs `ods check`
// evaluated. It performs no new detection or measurement.
func Build(in *policy.EvalInput, det *detector.DetectionResult, res *policy.EvalResult, meta Meta) *Document {
	ts := meta.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	stamp := ts.Format(time.RFC3339)

	changeName := "change"
	if meta.Repo != "" {
		changeName = meta.Repo
		if meta.PR != "" {
			changeName += " PR #" + meta.PR
		} else if meta.HeadSHA != "" {
			changeName += "@" + shortSHA(meta.HeadSHA)
		}
	}

	changeProps := []Property{}
	addProp := func(name, value string) {
		if value != "" {
			changeProps = append(changeProps, Property{Name: name, Value: value})
		}
	}
	addProp("ods:pr", meta.PR)
	addProp("ods:head_sha", meta.HeadSHA)
	addProp("ods:diff_base", meta.DiffBase)
	addProp("ods:branch", meta.Branch)
	addProp("ods:workflow_run", meta.RunURL)

	doc := &Document{
		BOMFormat:    "CycloneDX",
		SpecVersion:  "1.6",
		SerialNumber: "urn:uuid:" + newUUID(),
		Version:      1,
		Metadata: &Metadata{
			Timestamp: stamp,
			Tools: &Tools{Components: []Component{{
				Type: "application", Name: "ods", Version: meta.ToolVersion,
			}}},
			Component: &Component{
				BOMRef:     "change",
				Type:       "application",
				Name:       changeName,
				Properties: changeProps,
			},
		},
		Definitions:  &Definitions{Standards: []Standard{governanceStandard()}},
		Declarations: &Declarations{},
	}

	// --- components: every changed file (auditors want the denominator),
	// with identity evidence on the AI-attributed ones.
	aiByPath := map[string]policy.EvalFileInfo{}
	for _, f := range in.AIFiles {
		aiByPath[f.Path] = f
	}
	var methods []Method
	if det != nil {
		for _, ev := range det.Evidence {
			methods = append(methods, Method{
				Technique:  techniqueFor(ev.Source),
				Confidence: ev.Confidence,
				Value:      ev.Source + ": " + ev.Signal,
			})
		}
	}
	for _, path := range in.ChangedFiles {
		comp := Component{BOMRef: "file:" + path, Type: "file", Name: path}
		if f, ok := aiByPath[path]; ok {
			comp.Evidence = &ComponentEvidence{Identity: []Identity{{
				Field:          "name",
				Confidence:     f.Confidence,
				ConcludedValue: "ai-assisted",
				Methods:        methods,
			}}}
			comp.Properties = []Property{
				{Name: "ods:ai_lines", Value: fmt.Sprintf("%d", f.AILines)},
				{Name: "ods:total_lines", Value: fmt.Sprintf("%d", f.TotalLines)},
			}
			if in.EvidenceTier != "" {
				comp.Properties = append(comp.Properties, Property{Name: "ods:evidence_tier", Value: in.EvidenceTier})
			}
		}
		doc.Components = append(doc.Components, comp)
	}

	// --- declarations: target, claims, evidence, attestation rows.
	doc.Declarations.Targets = &Targets{Components: []Component{{
		BOMRef: "target:change", Type: "application", Name: changeName,
	}}}

	b := &declBuilder{doc: doc, stamp: stamp, runURL: meta.RunURL}

	// R1 — disclosure.
	tierConf, tierWhy := tierConfidence(in.EvidenceTier)
	if det != nil && det.AIGenerated {
		disclosed := in.EvidenceTier == "attested" || in.EvidenceTier == "corroborated"
		pred := "AI assistance on this change is disclosed via author/tool attribution."
		conf := 1.0
		if !disclosed {
			pred = "AI assistance is suspected from heuristics only; no author/tool attribution is present."
			conf = 0.0
		}
		b.row("req:ods-r1", "disclosed", pred,
			fmt.Sprintf("ods:detection — ai_generated=true confidence=%.2f sources=%v tier=%s", det.Confidence, det.Sources, in.EvidenceTier),
			&Scored{Score: conf, Rationale: "Disclosure-class attribution present: " + fmt.Sprint(disclosed)},
			&Scored{Score: tierConf, Rationale: tierWhy})
	} else {
		b.row("req:ods-r1", "disclosed", "No AI assistance was detected on this change.",
			"ods:detection — ai_generated=false; detection reads volunteered signals only",
			&Scored{Score: 1.0, Rationale: "Requirement is vacuously met: no AI assistance detected."},
			&Scored{Score: 0.5, Rationale: "Absence of a volunteered signal is not proof of absence."})
	}

	// R2 — evidence grading (always present; derivation is a pure function).
	b.row("req:ods-r2", "graded",
		fmt.Sprintf("Attribution strength is graded %q, derived deterministically from the detection sources.", tierLabel(in.EvidenceTier)),
		"ods:evidence_tier — pure function of detection_sources; reproducible from the commit history",
		&Scored{Score: 1.0, Rationale: "evidence_tier computed for every change."},
		&Scored{Score: 1.0, Rationale: "Derivation is deterministic and reproducible."})

	// R3 — diff-scoped test adequacy (patch coverage preferred; fall back to
	// the tests-touched fact when no per-line coverage report exists).
	switch {
	case in.PatchCoverage >= 0:
		b.row("req:ods-r3", "tested",
			fmt.Sprintf("%.0f%% of the diff's added lines are executed by the test suite.", in.PatchCoverage*100),
			"ods:patch_coverage — computed from the CI coverage artifact intersected with the diff's added lines",
			&Scored{Score: in.PatchCoverage, Rationale: "Patch coverage of added lines."},
			&Scored{Score: 1.0, Rationale: "Corroborated: computed from the coverage artifact of this run."})
	case in.MergeConfidence != nil && in.MergeConfidence.SourceFilesChanged > 0:
		score := 1.0
		pred := "Source changes are accompanied by test changes."
		if in.MergeConfidence.AddedSourceWithoutTests {
			score = 0.0
			pred = "Source code was added without any test file being added or updated."
		}
		b.row("req:ods-r3", "tested", pred,
			"ods:merge_confidence — deterministic facts from the diff (tests_touched, added_source_without_tests)",
			&Scored{Score: score, Rationale: "No per-line coverage report; based on the tests-touched fact."},
			&Scored{Score: 1.0, Rationale: "Corroborated: derived from the diff itself."})
	}

	// R4 — mutation adequacy (only when measured).
	if in.MutationScore >= 0 {
		b.row("req:ods-r4", "mutation",
			fmt.Sprintf("%.0f%% of mutants injected on the added lines are killed by the test suite.", in.MutationScore*100),
			"ods:mutation_score — diff-scoped mutation score from the CI mutation report",
			&Scored{Score: in.MutationScore, Rationale: "Diff-scoped mutation score."},
			&Scored{Score: 1.0, Rationale: "Corroborated: computed from the mutation-report artifact of this run."})
	}

	// R5 — policy evaluation.
	if res != nil {
		score := 0.0
		if res.Allowed {
			score = 1.0
		}
		tier := res.ReviewTier
		if tier == "" {
			tier = policy.ReviewTierStandard
		}
		b.row("req:ods-r5", "policy",
			fmt.Sprintf("The organization's policy evaluated the change: allowed=%t, review_tier=%s, warnings=%d, denials=%d.", res.Allowed, tier, len(res.Warnings), len(res.Denials)),
			"ods:policy — OPA evaluation of the assembled policy input",
			&Scored{Score: score, Rationale: "Policy verdict (allowed)."},
			&Scored{Score: 1.0, Rationale: "Corroborated: deterministic OPA evaluation in this run."})
	}

	// R6 — pipeline integrity (only when the CI wrapper reported it; the CLI
	// does not guess about stages it did not run).
	if meta.PipelineIntegrity != "" {
		score := 0.0
		if meta.PipelineIntegrity == "ok" {
			score = 1.0
		}
		b.row("req:ods-r6", "integrity",
			fmt.Sprintf("Pipeline integrity reported by the CI wrapper: %s.", meta.PipelineIntegrity),
			"ods:pipeline_integrity — per-stage completion status from the CI wrapper",
			&Scored{Score: score, Rationale: "All stages completed = ok; any substituted failure = inconclusive."},
			&Scored{Score: 1.0, Rationale: "Stage status is derived from the run itself."})
	}

	doc.Declarations.Attestations = []Attestation{{
		Summary: "ODS self-assessment of " + changeName + " against ODS AI-Assisted Code Governance v1",
		Map:     b.rows,
	}}
	doc.Declarations.Affirmation = &Affirmation{Statement: affirmationStatement}
	return doc
}

// declBuilder accumulates the linked claim/evidence/attestation triples.
type declBuilder struct {
	doc    *Document
	stamp  string
	runURL string
	rows   []AttestationMap
}

func (b *declBuilder) row(reqRef, key, predicate, evidenceDesc string, conformance, confidence *Scored) {
	evRef := "ev:" + key
	claimRef := "claim:" + key
	desc := evidenceDesc
	if b.runURL != "" {
		desc += ". Re-fetchable: " + b.runURL
	}
	b.doc.Declarations.Evidence = append(b.doc.Declarations.Evidence, EvidenceRef{
		BOMRef: evRef, PropertyName: "ods:" + key, Description: desc, Created: b.stamp,
	})
	b.doc.Declarations.Claims = append(b.doc.Declarations.Claims, Claim{
		BOMRef: claimRef, Target: "target:change", Predicate: predicate, Evidence: []string{evRef},
	})
	b.rows = append(b.rows, AttestationMap{
		Requirement: reqRef, Claims: []string{claimRef},
		Conformance: conformance, Confidence: confidence,
	})
}

func governanceStandard() Standard {
	return Standard{
		BOMRef:      "std:ods-governance-1",
		Name:        "ODS AI-Assisted Code Governance",
		Version:     "1",
		Owner:       "Open Delivery Spec",
		Description: "Deterministic, artifact-level requirements for governing AI-assisted changes: disclosure, evidence grading, diff-scoped test adequacy, policy evaluation, and pipeline integrity.",
		Requirements: []Requirement{
			{BOMRef: "req:ods-r1", Identifier: "ODS-R1", Title: "AI involvement is disclosed",
				Text: "When AI assistance is detected, author or tool attribution is present (commit trailer, PR-body disclosure, or git-ai notes)."},
			{BOMRef: "req:ods-r2", Identifier: "ODS-R2", Title: "Attribution strength is graded",
				Text: "The change carries an evidence tier stating how attribution was obtained: corroborated (measured), attested (declared), inferred (heuristic), or inconclusive."},
			{BOMRef: "req:ods-r3", Identifier: "ODS-R3", Title: "Changed code is exercised by tests",
				Text: "Source changes are accompanied by test changes, and the diff's added lines are covered (patch coverage) to the project's threshold."},
			{BOMRef: "req:ods-r4", Identifier: "ODS-R4", Title: "Tests detect faults in the added lines",
				Text: "Mutants injected on the diff's added lines are killed by the test suite (diff-scoped mutation score) to the project's threshold."},
			{BOMRef: "req:ods-r5", Identifier: "ODS-R5", Title: "Policy was evaluated and the change routed",
				Text: "The organization's OPA policy evaluated the change (allowed/denied) and produced a review-routing tier."},
			{BOMRef: "req:ods-r6", Identifier: "ODS-R6", Title: "Pipeline integrity",
				Text: "Every pipeline stage (detect, analyze, score, check) completed; no stage result was substituted for a failure."},
		},
	}
}

func tierLabel(tier string) string {
	if tier == "" {
		return "inconclusive"
	}
	return tier
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// newUUID returns a random RFC 4122 v4 UUID without external dependencies.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Marshal renders the document as indented JSON.
func (d *Document) Marshal() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}
