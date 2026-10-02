package gitai

import (
	"os"
	"testing"
)

// TestMain isolates every git command these tests run, directly or through
// the code under test, from the developer's global and system git config:
// a global hook, commit signing or a trace2 target would otherwise act on
// the fixture repositories and make results depend on the machine.
func TestMain(m *testing.M) {
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}
