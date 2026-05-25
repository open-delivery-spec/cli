package cmd

import (
	"testing"

	"github.com/open-delivery-spec/cli/internal/validator"
)

func TestPrintResultStrictTreatsWarningsAsErrors(t *testing.T) {
	originalStrict := strict
	t.Cleanup(func() {
		strict = originalStrict
	})

	strict = true
	err := printResult(validator.Result{
		Status:   validator.StatusConformantWarnings,
		Warnings: []string{"breaking change detected"},
	})
	if err == nil {
		t.Fatal("printResult() error = nil, want strict warning failure")
	}
}

func TestPrintResultAllowsWarningsWithoutStrict(t *testing.T) {
	originalStrict := strict
	t.Cleanup(func() {
		strict = originalStrict
	})

	strict = false
	err := printResult(validator.Result{
		Status:   validator.StatusConformantWarnings,
		Warnings: []string{"breaking change detected"},
	})
	if err != nil {
		t.Fatalf("printResult() error = %v, want nil", err)
	}
}
