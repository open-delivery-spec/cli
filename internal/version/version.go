package version

// Build-time variables injected by GoReleaser ldflags.
var Version = "dev"
var Value = "dev" // root.go reads this; goreleaser sets both Version and Value
var Commit = "unknown"
var Date = "unknown"
