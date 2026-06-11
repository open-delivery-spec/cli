package policy

import (
	"os"
	"strings"
	"testing"
)

func TestEvaluateDefaultPolicy(t *testing.T) {
	// Write default policy to temp file
	tmpFile, err := os.CreateTemp("", "ods-policy-test-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(DefaultRegoPolicy()); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Test 1: Clean input - should pass
	t.Run("clean input passes", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:        false,
			AIConfidence:       0,
			TechnicalDebtDelta: 0.5,
			TestCoverage:       0.8,
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if !result.Allowed {
			t.Errorf("expected allowed=true, got false with denials: %v", result.Denials)
		}
	})

	// Test 2: Critical issues should block
	t.Run("critical issues block", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:        true,
			AIConfidence:       0.9,
			TechnicalDebtDelta: 1.0,
			TestCoverage:       0.5,
			Issues: []EvalIssue{
				{Rule: "ai-hallucinated-api", File: "auth.go", Line: 10, Severity: "critical", Message: "API does not exist"},
			},
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if result.Allowed {
			t.Error("expected allowed=false for critical issues")
		}
	})

	// Test 3: High tech debt blocks
	t.Run("high tech debt blocks", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:        true,
			AIConfidence:       0.9,
			TechnicalDebtDelta: 6.5,
			TestCoverage:       0.1,
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if result.Allowed {
			t.Error("expected allowed=false for high tech debt")
		}
	})

	// Test 4: AI with low coverage warns
	t.Run("ai low coverage warns", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:        true,
			AIConfidence:       0.85,
			TechnicalDebtDelta: 1.0,
			TestCoverage:       0.15,
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if !result.Allowed {
			t.Errorf("expected allowed=true for warning-only case, got denials: %v", result.Denials)
		}
		if len(result.Warnings) == 0 {
			t.Error("expected warnings for low-coverage AI code")
		}
	})
}

func TestEvaluateEnterprisePolicy(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "ods-enterprise-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(EnterpriseRegoPolicy()); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	t.Run("payment module AI code low coverage blocks", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:  true,
			AIConfidence: 0.9,
			TestCoverage: 0.3,
			AIFiles: []EvalFileInfo{
				{Path: "src/payment/handler.go", AILines: 50, TotalLines: 80, Confidence: 0.85},
			},
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if result.Allowed {
			t.Error("expected blocked for payment module AI code with low coverage")
		}
	})

	t.Run("non-sensitive module AI code passes", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:  true,
			AIConfidence: 0.6,
			TestCoverage: 0.7,
			AIFiles: []EvalFileInfo{
				{Path: "src/utils/helpers.go", AILines: 20, TotalLines: 100, Confidence: 0.5},
			},
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if !result.Allowed {
			t.Errorf("expected allowed for non-sensitive module, got denials: %v", result.Denials)
		}
	})
}

func TestParseRegoResults(t *testing.T) {
	t.Run("empty results default to allowed", func(t *testing.T) {
		result, err := parseRegoResults(nil)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Allowed {
			t.Error("empty results should default to allowed")
		}
	})
}

func TestExtractStringList(t *testing.T) {
	t.Run("string slice", func(t *testing.T) {
		input := []interface{}{"a", "b", "c"}
		result := extractStringList(input)
		if len(result) != 3 {
			t.Errorf("len = %d, want 3", len(result))
		}
	})

	t.Run("single string", func(t *testing.T) {
		result := extractStringList("hello")
		if len(result) != 1 || result[0] != "hello" {
			t.Errorf("result = %v, want [hello]", result)
		}
	})
}

func TestDiscoverRegoFile(t *testing.T) {
	// Create temp dir with policy
	dir := t.TempDir()
	policyDir := dir + "/.ods"
	os.MkdirAll(policyDir, 0755)
	os.WriteFile(policyDir+"/policy.rego", []byte("package ods.policy"), 0644)

	path := DiscoverRegoFile(dir)
	if path == "" {
		t.Error("DiscoverRegoFile should find .ods/policy.rego")
	}
	if !strings.HasSuffix(path, ".ods/policy.rego") {
		t.Errorf("path = %s, want ending with .ods/policy.rego", path)
	}
}

func TestDefaultRegoPolicy(t *testing.T) {
	policy := DefaultRegoPolicy()
	if !strings.Contains(policy, "package ods.policy") {
		t.Error("default policy missing package declaration")
	}
	if !strings.Contains(policy, "deny") {
		t.Error("default policy missing deny rules")
	}
}

func TestEnterpriseRegoPolicy(t *testing.T) {
	policy := EnterpriseRegoPolicy()
	if !strings.Contains(policy, "payment|auth|billing") {
		t.Error("enterprise policy missing sensitive module check")
	}
}
