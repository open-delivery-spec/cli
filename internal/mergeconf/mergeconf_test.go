package mergeconf

import (
	"reflect"
	"sort"
	"testing"
)

func TestIsTestFile(t *testing.T) {
	cases := map[string]bool{
		"internal/foo_test.go":          true,
		"pkg/util.go":                   false,
		"app/tests/test_login.py":       true,
		"src/auth.test.ts":              true,
		"src/auth.spec.tsx":             true,
		"lib/user_spec.rb":              true,
		"src/__tests__/button.jsx":      true,
		"com/example/UserTest.java":     true,
		"Widget.Tests.cs":               true,
		"src/main.rs":                   false,
		"crates/x/tests/integration.rs": true,
		"test_helpers.py":               true,
		"":                              false,
		"README.md":                     false,
	}
	for p, want := range cases {
		if got := IsTestFile(p); got != want {
			t.Errorf("IsTestFile(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestIsRiskyPath(t *testing.T) {
	cases := map[string]bool{
		".github/workflows/ci.yml":     true,
		".github/actions/x/action.yml": true,
		"go.mod":                       true,
		"go.sum":                       true,
		"package-lock.json":            true,
		"requirements-dev.txt":         true,
		"infra/main.tf":                true,
		"internal/auth/session.go":     true,
		"src/crypto/aes.rs":            true,
		"src/handlers/user.go":         false,
		"docs/guide.md":                false,
		"Dockerfile":                   true,
		"":                             false,
	}
	for p, want := range cases {
		if got := IsRiskyPath(p); got != want {
			t.Errorf("IsRiskyPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestCompute_addedSourceWithoutTests(t *testing.T) {
	changed := []string{"internal/svc.go", "cmd/main.go"}
	added := map[string]int{"internal/svc.go": 40, "cmd/main.go": 10}
	s := Compute(changed, added)
	if !s.AddedSourceWithoutTests {
		t.Error("expected AddedSourceWithoutTests=true (source added, no test file)")
	}
	if s.TestsTouched {
		t.Error("expected TestsTouched=false")
	}
	if s.SourceFilesChanged != 2 || s.TestFilesChanged != 0 {
		t.Errorf("source/test counts = %d/%d, want 2/0", s.SourceFilesChanged, s.TestFilesChanged)
	}
	if s.NetAddedLines != 50 || s.FilesChanged != 2 {
		t.Errorf("net=%d files=%d, want 50/2", s.NetAddedLines, s.FilesChanged)
	}
}

func TestCompute_testsTouchedClearsSignal(t *testing.T) {
	changed := []string{"internal/svc.go", "internal/svc_test.go"}
	added := map[string]int{"internal/svc.go": 40, "internal/svc_test.go": 30}
	s := Compute(changed, added)
	if s.AddedSourceWithoutTests {
		t.Error("a touched test file must clear AddedSourceWithoutTests")
	}
	if !s.TestsTouched {
		t.Error("expected TestsTouched=true")
	}
	if s.SourceFilesChanged != 1 || s.TestFilesChanged != 1 {
		t.Errorf("source/test counts = %d/%d, want 1/1", s.SourceFilesChanged, s.TestFilesChanged)
	}
}

func TestCompute_testOnlyChangeIsNotUndertested(t *testing.T) {
	// Adding only tests must never flag "added source without tests".
	changed := []string{"internal/svc_test.go"}
	added := map[string]int{"internal/svc_test.go": 30}
	s := Compute(changed, added)
	if s.AddedSourceWithoutTests {
		t.Error("test-only change must not be AddedSourceWithoutTests")
	}
}

func TestCompute_riskyPaths(t *testing.T) {
	changed := []string{"internal/auth/session.go", ".github/workflows/ci.yml", "README.md"}
	added := map[string]int{"internal/auth/session.go": 12}
	s := Compute(changed, added)
	got := append([]string(nil), s.RiskyPaths...)
	sort.Strings(got)
	want := []string{".github/workflows/ci.yml", "internal/auth/session.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RiskyPaths = %v, want %v", got, want)
	}
}

func TestCompute_empty(t *testing.T) {
	s := Compute(nil, nil)
	if s.FilesChanged != 0 || s.AddedSourceWithoutTests || s.TestsTouched || len(s.RiskyPaths) != 0 {
		t.Errorf("empty diff produced non-zero signals: %+v", s)
	}
}
