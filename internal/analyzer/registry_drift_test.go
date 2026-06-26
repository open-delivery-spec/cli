package analyzer

import (
	"testing"

	"github.com/open-delivery-spec/cli/internal/rules"
)

// TestEmittedRulesAreRegistered runs the analyzer over inputs crafted to trigger
// several rules and asserts every emitted rule ID exists in the registry. This
// ties the analyzer's actual output to the rules catalogue so the two cannot drift.
func TestEmittedRulesAreRegistered(t *testing.T) {
	files := map[string][]string{
		// Over-commenting: comment-heavy file.
		"comments.go": {
			"// one", "// two", "// three", "// four", "// five",
			"package x", "func A() {}",
		},
		// Redundant error handling: dense if-err-nil blocks.
		"errs.go": {
			"a, err := f()", "if err != nil {", "return err", "}",
			"b, err := g()", "if err != nil {", "return err", "}",
		},
		// Unsafe deserialization: json.Unmarshal into interface{}.
		"unmarshal.go": {
			"var data interface{}",
			"json.Unmarshal(raw, &data)",
		},
	}

	res := Analyze(Options{Files: files})
	if len(res.Issues) == 0 {
		t.Fatal("expected the crafted inputs to produce issues")
	}
	for _, iss := range res.Issues {
		if _, ok := rules.Get(iss.Rule); !ok {
			t.Errorf("analyzer emitted rule %q that is not in the registry", iss.Rule)
		}
	}
}
