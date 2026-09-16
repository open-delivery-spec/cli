package version

// Build-time variables injected by GoReleaser ldflags (see .goreleaser.yml).
// `ods --version` prints all three.
var (
	Value  = "dev"     // release version
	Commit = "unknown" // git commit the binary was built from
	Date   = "unknown" // build date
)
