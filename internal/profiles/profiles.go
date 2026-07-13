// Package profiles provides ready-made ODS policy profiles — curated OPA Rego
// starting points so teams don't face a blank policy file.
//
// They are the ODS analogue of SonarQube's built-in quality profiles: a
// recommended default ("ods-way"), a strict profile for regulated codebases,
// and a non-blocking "advisory" profile for incremental adoption. `ods init
// --profile <name>` writes the chosen one to .ods/policy.rego.
package profiles

import (
	_ "embed"
	"fmt"
	"sort"
)

//go:embed ods-way.rego
var odsWay string

//go:embed strict.rego
var strict string

//go:embed advisory.rego
var advisory string

// Default is the profile written when none is specified.
const Default = "ods-way"

// Profile describes a named policy starting point.
type Profile struct {
	Name    string
	Summary string
	Policy  string
}

var registry = map[string]Profile{
	"ods-way": {
		Name:    "ods-way",
		Summary: "Recommended default — block critical issues, surface the rest, route by risk.",
		Policy:  odsWay,
	},
	"strict": {
		Name:    "strict",
		Summary: "Regulated/high-stakes — also block high-severity issues and low-coverage AI code.",
		Policy:  strict,
	},
	"advisory": {
		Name:    "advisory",
		Summary: "Never blocks — warns and routes only. For incremental adoption / building trust.",
		Policy:  advisory,
	},
}

// Get returns the profile with the given name, or an error naming the valid
// choices.
func Get(name string) (Profile, error) {
	p, ok := registry[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown profile %q (choose one of: %v)", name, Names())
	}
	return p, nil
}

// Names returns the profile names, sorted, with the default first.
func Names() []string {
	rest := make([]string, 0, len(registry)-1)
	for n := range registry {
		if n != Default {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	return append([]string{Default}, rest...)
}

// All returns every profile, default first then the rest sorted by name.
func All() []Profile {
	out := make([]Profile, 0, len(registry))
	for _, n := range Names() {
		out = append(out, registry[n])
	}
	return out
}
