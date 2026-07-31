// Package mergeconf computes deterministic, diff-scoped "merge-confidence"
// signals about a change: is it tested, how is it shaped, does it touch
// sensitive paths. These are facts derived from the diff alone — no LLM, no
// stylometric guessing. They are advisory by default: a policy routes review
// attention on them and may opt in to deny; attribution (whether the change is
// AI-authored) is used elsewhere to raise the bar, never here to detect.
package mergeconf

import (
	"path"
	"strings"
)

// Signals are the deterministic merge-confidence facts about a diff.
type Signals struct {
	// FilesChanged is the number of files touched (all types).
	FilesChanged int `json:"files_changed"`
	// SourceFilesChanged / TestFilesChanged count code files by kind.
	SourceFilesChanged int `json:"source_files_changed"`
	TestFilesChanged   int `json:"test_files_changed"`
	// NetAddedLines is the total added code lines across changed code files.
	NetAddedLines int `json:"net_added_lines"`
	// TestsTouched is true when any changed file is a test file.
	TestsTouched bool `json:"tests_touched"`
	// AddedSourceWithoutTests is true when source code was added but no test
	// file was added or modified — the community's most-cited "AI PR" smell.
	AddedSourceWithoutTests bool `json:"added_source_without_tests"`
	// RiskyPaths lists changed files on sensitive paths (CI config, dependency
	// manifests/lockfiles, security-related paths).
	RiskyPaths []string `json:"risky_paths,omitempty"`
}

// Compute derives the signals from the full list of changed paths and the
// per-code-file added-line counts (as produced by the diff plumbing). It is
// pure so it can be unit-tested without invoking git.
func Compute(changedFiles []string, addedByCodeFile map[string]int) Signals {
	s := Signals{FilesChanged: len(changedFiles)}

	for _, f := range changedFiles {
		if IsTestFile(f) {
			s.TestsTouched = true
		}
		if IsRiskyPath(f) {
			s.RiskyPaths = append(s.RiskyPaths, f)
		}
	}

	sourceAdded := 0
	for f, n := range addedByCodeFile {
		s.NetAddedLines += n
		if IsTestFile(f) {
			s.TestFilesChanged++
		} else {
			s.SourceFilesChanged++
			sourceAdded += n
		}
	}

	s.AddedSourceWithoutTests = sourceAdded > 0 && !s.TestsTouched
	return s
}

// testFileSuffixes match on the file's base name (case-insensitive).
var testFileSuffixes = []string{
	"_test.go",
	"_test.py", "_test.rb", "_test.rs", "_test.exs", "_test.ex",
	".test.js", ".test.jsx", ".test.ts", ".test.tsx", ".test.mjs",
	".spec.js", ".spec.jsx", ".spec.ts", ".spec.tsx", ".spec.mjs",
	"_spec.rb",
	"test.java", "tests.java", "test.kt", "tests.kt",
	"test.cs", "tests.cs",
	"spec.hs",
}

// testFilePrefixes match on the file's base name (case-insensitive).
var testFilePrefixes = []string{"test_", "test-"}

// testDirSegments: a path segment that marks a test directory.
var testDirSegments = map[string]bool{
	"test": true, "tests": true, "__tests__": true,
	"spec": true, "specs": true, "testing": true,
}

// IsTestFile reports whether a path looks like a test file across common
// languages, by base-name convention or by living under a test directory.
func IsTestFile(p string) bool {
	lp := strings.ToLower(strings.TrimSpace(p))
	if lp == "" {
		return false
	}
	base := path.Base(lp)
	for _, suf := range testFileSuffixes {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	for _, pre := range testFilePrefixes {
		if strings.HasPrefix(base, pre) {
			return true
		}
	}
	for _, seg := range strings.Split(lp, "/") {
		if testDirSegments[seg] {
			return true
		}
	}
	return false
}

// riskyExactNames are dependency manifests / lockfiles whose base name matches.
var riskyExactNames = map[string]bool{
	"go.mod": true, "go.sum": true,
	"package.json": true, "package-lock.json": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "npm-shrinkwrap.json": true,
	"cargo.toml": true, "cargo.lock": true,
	"gemfile": true, "gemfile.lock": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
	"composer.json": true, "composer.lock": true,
	"pyproject.toml": true, "poetry.lock": true, "pipfile": true, "pipfile.lock": true,
	"dockerfile": true, "docker-compose.yml": true, "docker-compose.yaml": true,
}

// riskyBasePrefixes match dependency files by base-name prefix.
var riskyBasePrefixes = []string{"requirements"} // requirements.txt, requirements-dev.txt, …

// riskySegments are path segments that flag a security-sensitive area.
var riskySegments = map[string]bool{
	"auth": true, "authentication": true, "authorization": true,
	"crypto": true, "cryptography": true, "security": true,
	"secret": true, "secrets": true, "credential": true, "credentials": true,
	"password": true, "session": true, "oauth": true, "jwt": true,
}

// IsRiskyPath reports whether a changed path is security/CI/dependency
// sensitive — the kind of change that warrants extra scrutiny regardless of
// size. Deterministic, path-based only.
func IsRiskyPath(p string) bool {
	lp := strings.ToLower(strings.TrimSpace(p))
	if lp == "" {
		return false
	}
	// CI / workflow configuration.
	if strings.HasPrefix(lp, ".github/workflows/") || strings.HasPrefix(lp, ".github/actions/") {
		return true
	}
	if strings.HasSuffix(lp, ".tf") || strings.HasSuffix(lp, ".tfvars") {
		return true
	}
	base := path.Base(lp)
	if riskyExactNames[base] {
		return true
	}
	for _, pre := range riskyBasePrefixes {
		if strings.HasPrefix(base, pre) {
			return true
		}
	}
	for _, seg := range strings.Split(lp, "/") {
		if riskySegments[seg] {
			return true
		}
	}
	return false
}
