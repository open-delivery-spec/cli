package version

// Value is the semantic version of the CLI, set via ldflags at build time.
// Default value is "dev" for development builds.
// Example: go build -ldflags="-X github.com/open-delivery-spec/cli/internal/version.Value=v0.2.1"
var Value = "dev"
