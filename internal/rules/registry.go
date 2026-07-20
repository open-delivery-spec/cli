// Package rules is the single source of truth for ODS analysis rule metadata.
//
// Each rule the analyzer can emit is registered here with a stable ID,
// description, default severity, and remediation suggestion. The analyzer
// references these IDs so the catalogue and the implementation cannot drift,
// and the `ods rules` command renders the registry for discovery.
package rules

// Stable rule identifiers. The analyzer emits these exact strings, and the
// registry below documents each one. Keep the two in sync — TestRegistryMatchesAnalyzer
// guards against drift.
const (
	RedundantErrorHandling = "ai-redundant-error-handling"
	OverCommenting         = "ai-over-commenting"
	UnsafeDeserialization  = "ai-unsafe-deserialization"
	InconsistentPattern    = "ai-inconsistent-pattern"
	HallucinatedAPI        = "ai-hallucinated-api"
)

// Rule describes a single analysis rule.
type Rule struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	DefaultSeverity string `json:"default_severity"` // critical, high, medium, low, info
	Category        string `json:"category"`         // e.g. "ai-pattern"
	Suggestion      string `json:"suggestion"`
}

// registry is the ordered catalogue of all built-in analysis rules.
var registry = []Rule{
	{
		ID:              RedundantErrorHandling,
		Name:            "Redundant error handling",
		Description:     "Dense clusters of if-err-nil blocks in close proximity. Informational only — dense error checks are idiomatic Go, not a defect.",
		DefaultSeverity: "info",
		Category:        "ai-pattern",
		Suggestion:      "If these checks are repetitive boilerplate, consider a helper; otherwise this is normal Go.",
	},
	{
		ID:              OverCommenting,
		Name:            "Over-commenting",
		Description:     "Comment-to-code ratio at or above 40%. Informational only — a style signal, not a defect; documented public APIs legitimately exceed this.",
		DefaultSeverity: "info",
		Category:        "ai-pattern",
		Suggestion:      "If comments restate the code, prefer explaining why over what; documentation comments are fine.",
	},
	{
		ID:              UnsafeDeserialization,
		Name:            "Unsafe deserialization",
		Description:     "json.Unmarshal into interface{} without type validation — AI commonly skips type checking.",
		DefaultSeverity: "high",
		Category:        "ai-pattern",
		Suggestion:      "Use a concrete struct type or validate the unmarshalled data before use.",
	},
	{
		ID:              InconsistentPattern,
		Name:            "Inconsistent pattern",
		Description:     "Mixed naming conventions or indentation styles — AI can blend styles from different training sources.",
		DefaultSeverity: "medium",
		Category:        "ai-pattern",
		Suggestion:      "Standardize on one naming convention and indentation style; run gofmt/prettier.",
	},
	{
		ID:              HallucinatedAPI,
		Name:            "Hallucinated or deprecated API",
		Description:     "Use of deprecated Go standard library APIs that AI commonly generates from older training data: io/ioutil (deprecated Go 1.16) and rand.Seed (deprecated Go 1.20).",
		DefaultSeverity: "medium",
		Category:        "ai-pattern",
		Suggestion:      "Replace io/ioutil with io and os equivalents (io.ReadAll, os.ReadFile, os.WriteFile, io.NopCloser). Remove rand.Seed — the global source is automatically seeded since Go 1.20.",
	},
}

// All returns a copy of the full rule catalogue in registry order.
func All() []Rule {
	out := make([]Rule, len(registry))
	copy(out, registry)
	return out
}

// Get returns the rule with the given ID and whether it was found.
func Get(id string) (Rule, bool) {
	for _, r := range registry {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// IDs returns all rule IDs in registry order.
func IDs() []string {
	out := make([]string, len(registry))
	for i, r := range registry {
		out[i] = r.ID
	}
	return out
}
