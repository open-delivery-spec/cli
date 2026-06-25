package version

// Version is the semantic version string, injected at build time by GoReleaser.
// Kept as "Version" for GoReleaser compatibility; Value is an alias used by root.go.
var Version = "dev"

// Value is the variable root.go reads; GoReleaser sets Version, which must match.
// Both point to the same ldflags target via the goreleaser.yml configuration.
var Value = "dev"

// Commit is the git commit SHA, injected at build time.
var Commit = "unknown"

// Date is the build date in RFC3339 format, injected at build time.
var Date = "unknown"
