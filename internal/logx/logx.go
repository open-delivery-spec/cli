// Package logx provides lightweight, opt-in debug logging for the ods CLI.
//
// Debug output always goes to stderr so it never pollutes the JSON written to
// stdout by the commands. Logging is disabled by default and enabled via the
// global --debug flag or the ODS_DEBUG environment variable.
package logx

import (
	"fmt"
	"io"
	"os"
)

var (
	enabled bool
	out     io.Writer = os.Stderr
)

// SetEnabled turns debug logging on or off.
func SetEnabled(v bool) { enabled = v }

// Enabled reports whether debug logging is currently on.
func Enabled() bool { return enabled }

// SetOutput redirects debug output. Used by tests; defaults to os.Stderr.
func SetOutput(w io.Writer) { out = w }

// Debugf writes a formatted debug line (prefixed and newline-terminated) to the
// debug output when logging is enabled. It is a no-op otherwise.
func Debugf(format string, args ...interface{}) {
	if !enabled {
		return
	}
	fmt.Fprintf(out, "[ods:debug] "+format+"\n", args...)
}
