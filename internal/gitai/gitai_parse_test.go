package gitai

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// TestParseNote_Entries guards how attestation lines are classified and
// attributed: which keys count as AI or human, which lines are ignored, and
// how per-file counts and agent labels accumulate.
func TestParseNote_Entries(t *testing.T) {
	const meta = `{
  "schema_version": "authorship/3.0.0",
  "sessions": {
    "s_1": {"agent_id": {"tool": "cursor", "model": "m1"}},
    "s_2": {"agent_id": {"tool": "claude"}}
  }
}`
	cases := []struct {
		name       string
		attest     string
		wantAI     int
		wantHuman  int
		wantFiles  map[string]int
		wantAgents []string
	}{
		{
			name:      "empty attestation section",
			attest:    "",
			wantFiles: map[string]int{},
		},
		{
			// The second entry belongs to a real file; the first has no file to
			// belong to, and its session must not leak into the agent list.
			name:       "entry before any file path is ignored",
			attest:     "  s_2::t_9 1-5\nreal.go\n  s_1::t_1 7",
			wantAI:     1,
			wantFiles:  map[string]int{"real.go": 1},
			wantAgents: []string{"cursor/m1"},
		},
		{
			name:       "entry without line ranges is ignored",
			attest:     "a.go\n  s_1::t_1\n  s_1::t_1 2",
			wantAI:     1,
			wantFiles:  map[string]int{"a.go": 1},
			wantAgents: []string{"cursor/m1"},
		},
		{
			name:       "blank lines between blocks are skipped",
			attest:     "a.go\n  s_1::t_1 1-2\n\nb.go\n\n  s_1::t_1 3\n",
			wantAI:     3,
			wantFiles:  map[string]int{"a.go": 2, "b.go": 1},
			wantAgents: []string{"cursor/m1"},
		},
		{
			// h_ is human, s_ (with or without a trace id), "::" and a 16-hex
			// legacy key are AI, and any other key is neither.
			name: "key classification",
			attest: "a.go\n" +
				"  h_abc 1-4\n" +
				"  s_solo 1-2\n" +
				"  0123456789abcdef 7\n" +
				"  p_1::t_2 9-10\n" +
				"  mystery 11-20",
			wantAI:    5,
			wantHuman: 4,
			wantFiles: map[string]int{"a.go": 5},
		},
		{
			name: "legacy key must be exactly 16 lowercase hex digits",
			attest: "a.go\n" +
				"  0123456789abcde 1\n" + // 15 digits
				"  0123456789abcdef0 2\n" + // 17 digits
				"  0123456789ABCDEF 3\n" + // upper case
				"  0123456789abcdeg 4", // not hex
			wantFiles: map[string]int{},
		},
		{
			name:      "human-only file has no AI entry",
			attest:    "a.go\n  h_abc 1-4",
			wantHuman: 4,
			wantFiles: map[string]int{},
		},
		{
			name:       "same file listed twice accumulates",
			attest:     "a.go\n  s_1::t_1 1-2\n  s_2::t_2 5\na.go\n  s_1::t_1 9",
			wantAI:     4,
			wantFiles:  map[string]int{"a.go": 4},
			wantAgents: []string{"claude", "cursor/m1"},
		},
		{
			// s_3 is not declared in the metadata: its lines still count as AI,
			// it just has no label. s_1 appears in two files, listed once.
			name:       "agents are unique, sorted, and only resolvable sessions are listed",
			attest:     "b.go\n  s_2::t_1 1\na.go\n  s_1::t_1 1\n  s_3::t_1 2\nc.go\n  s_1::t_2 3",
			wantAI:     4,
			wantFiles:  map[string]int{"a.go": 2, "b.go": 1, "c.go": 1},
			wantAgents: []string{"claude", "cursor/m1"},
		},
		{
			name:       "quoted path keeps inner spaces and drops padding",
			attest:     "\"dir/my file.go\"  \n  s_1::t_1 1-2",
			wantAI:     2,
			wantFiles:  map[string]int{"dir/my file.go": 2},
			wantAgents: []string{"cursor/m1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ca, err := parseNote(tc.attest + "\n---\n" + meta)
			if err != nil {
				t.Fatalf("parseNote: %v", err)
			}
			if ca.AILines != tc.wantAI {
				t.Errorf("AILines = %d, want %d", ca.AILines, tc.wantAI)
			}
			if ca.HumanLines != tc.wantHuman {
				t.Errorf("HumanLines = %d, want %d", ca.HumanLines, tc.wantHuman)
			}
			if !maps.Equal(ca.Files, tc.wantFiles) {
				t.Errorf("Files = %v, want %v", ca.Files, tc.wantFiles)
			}
			if !slices.Equal(ca.Agents, tc.wantAgents) {
				t.Errorf("Agents = %v, want %v", ca.Agents, tc.wantAgents)
			}
		})
	}
}

// TestParseNote_AgentsAreSortedAndUnique guards the deterministic agent list:
// however many sessions a log has, Agents comes out sorted with each label
// once, even though the sessions are collected in a map. The log is parsed
// repeatedly because map order is random, so an unsorted result would match
// by chance now and then.
func TestParseNote_AgentsAreSortedAndUnique(t *testing.T) {
	note := authorshipLog(`
a.go
  s_d::t_1 1
  s_b::t_1 2
  s_a::t_1 3
  s_c::t_1 4
  s_e::t_1 5`,
		sessionJSON("s_a", "zed", "m"),
		sessionJSON("s_b", "amp", "m"),
		sessionJSON("s_c", "cursor", "m"),
		sessionJSON("s_d", "claude", "m"),
		sessionJSON("s_e", "amp", "m"), // the same label as s_b
	)
	want := []string{"amp/m", "claude/m", "cursor/m", "zed/m"}
	for range 20 {
		ca, err := parseNote(note)
		if err != nil {
			t.Fatalf("parseNote: %v", err)
		}
		if !slices.Equal(ca.Agents, want) {
			t.Fatalf("Agents = %v, want %v", ca.Agents, want)
		}
	}
}

// TestParseNote_NoSessionMetadata guards the tolerance for a log whose
// metadata declares no sessions: the AI lines are still counted, only the
// agent labels are missing.
func TestParseNote_NoSessionMetadata(t *testing.T) {
	ca, err := parseNote("a.go\n  s_1::t_1 1-3\n---\n" + `{"schema_version":"authorship/3.0.0"}`)
	if err != nil {
		t.Fatalf("parseNote: %v", err)
	}
	if ca.AILines != 3 || ca.Files["a.go"] != 3 {
		t.Errorf("AILines = %d, Files = %v, want 3 lines in a.go", ca.AILines, ca.Files)
	}
	if len(ca.Agents) != 0 {
		t.Errorf("Agents = %v, want none without a sessions map", ca.Agents)
	}
}

// TestParseNote_Errors guards each way a log is rejected, and that the error
// says which part was wrong: missing separator, unreadable metadata, or a
// schema that is not the authorship log format.
func TestParseNote_Errors(t *testing.T) {
	cases := []struct {
		name, note, wantErr string
	}{
		{"empty note", "", "missing --- separator"},
		{"attestation only", "a.go\n  s_1::t_1 1", "missing --- separator"},
		{"separator with trailing text is not a separator", "a.go\n--- \n{}", "missing --- separator"},
		{"no metadata after the separator", "a.go\n---", "parsing authorship metadata"},
		{"metadata is not an object", "a.go\n---\n[1, 2]", "parsing authorship metadata"},
		{"metadata is truncated", "a.go\n---\n{\"schema_version\":", "parsing authorship metadata"},
		{"metadata without a schema version", "a.go\n---\n{}", "unrecognized schema_version"},
		{"foreign schema version", "a.go\n---\n{\"schema_version\":\"other/1.0\"}", "unrecognized schema_version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ca, err := parseNote(tc.note)
			if err == nil {
				t.Fatalf("parseNote(%q) = %+v, want an error", tc.note, ca)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestSessionLabel_Unresolvable guards the labels sessionLabel refuses to
// invent: a record that names no tool, a model without a tool, and a session
// the metadata does not declare all resolve to the empty label, while a key
// without a trace id still resolves its session.
func TestSessionLabel_Unresolvable(t *testing.T) {
	sessions := map[string]sessionRecord{
		"s_full":  {AgentID: agentID{Tool: "cursor", Model: "gpt-4"}},
		"s_model": {AgentID: agentID{Model: "gpt-4"}},
		"s_empty": {},
	}
	cases := []struct {
		name     string
		key      string
		sessions map[string]sessionRecord
		want     string
	}{
		{"model without a tool", "s_model::t_1", sessions, ""},
		{"record without tool or model", "s_empty::t_1", sessions, ""},
		{"undeclared session", "s_nope::t_1", sessions, ""},
		{"key without a trace id", "s_full", sessions, "cursor/gpt-4"},
		{"no sessions at all", "s_full::t_1", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionLabel(tc.key, tc.sessions); got != tc.want {
				t.Errorf("sessionLabel(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}
