package logx

import (
	"bytes"
	"strings"
	"testing"
)

// reset restores package state after a test mutates the globals.
func reset() {
	enabled = false
	out = nil
}

func TestDebugf_DisabledByDefault(t *testing.T) {
	t.Cleanup(reset)
	var buf bytes.Buffer
	SetOutput(&buf)
	// enabled defaults to false
	Debugf("should not appear: %d", 42)
	if buf.Len() != 0 {
		t.Errorf("expected no output when disabled, got %q", buf.String())
	}
}

func TestDebugf_Enabled(t *testing.T) {
	t.Cleanup(reset)
	var buf bytes.Buffer
	SetOutput(&buf)
	SetEnabled(true)
	Debugf("value=%d name=%s", 7, "x")
	got := buf.String()
	if !strings.Contains(got, "value=7 name=x") {
		t.Errorf("output missing formatted message: %q", got)
	}
	if !strings.HasPrefix(got, "[ods:debug] ") {
		t.Errorf("output missing prefix: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output not newline-terminated: %q", got)
	}
}

func TestSetEnabledAndEnabled(t *testing.T) {
	t.Cleanup(reset)
	SetEnabled(false)
	if Enabled() {
		t.Error("Enabled() = true after SetEnabled(false)")
	}
	SetEnabled(true)
	if !Enabled() {
		t.Error("Enabled() = false after SetEnabled(true)")
	}
}

func TestDebugf_TogglesOff(t *testing.T) {
	t.Cleanup(reset)
	var buf bytes.Buffer
	SetOutput(&buf)
	SetEnabled(true)
	Debugf("first")
	SetEnabled(false)
	Debugf("second")
	got := buf.String()
	if !strings.Contains(got, "first") {
		t.Errorf("expected first message, got %q", got)
	}
	if strings.Contains(got, "second") {
		t.Errorf("second message should be suppressed, got %q", got)
	}
}
