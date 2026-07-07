package gitai

import "testing"

// sampleNote mirrors the Git AI Standard v3 example: a session entry, a human
// entry, and a legacy-key entry across two files.
const sampleNote = `src/main.rs
  s_c9883b05a2487d::t_9f8e7d6c5b4a32 1-10,15-20
  h_31dce776f88375 42-50
"src/my file.rs"
  0123456789abcdef 5,7-9
---
{
  "schema_version": "authorship/3.0.0",
  "base_commit_sha": "7734793b756b3921c88db5375a8c156e9532447b",
  "prompts": {},
  "sessions": {
    "s_c9883b05a2487d": {
      "agent_id": {
        "tool": "cursor",
        "id": "6ef2299e-a67f-432b-aa80-3d2fb4d28999",
        "model": "claude-sonnet-4-5-20250514"
      },
      "human_author": "dev@example.com"
    }
  },
  "humans": {
    "h_31dce776f88375": {
      "author": "Developer <dev@example.com>"
    }
  }
}`

func TestParseNote(t *testing.T) {
	ca, err := parseNote(sampleNote)
	if err != nil {
		t.Fatalf("parseNote: %v", err)
	}
	// Session: 1-10 (10) + 15-20 (6) = 16; legacy: 5,7-9 = 4 → 20 AI lines.
	if ca.AILines != 20 {
		t.Errorf("AILines = %d, want 20", ca.AILines)
	}
	// Human: 42-50 = 9, not counted as AI.
	if ca.HumanLines != 9 {
		t.Errorf("HumanLines = %d, want 9", ca.HumanLines)
	}
	if got := ca.Files["src/main.rs"]; got != 16 {
		t.Errorf("main.rs AI lines = %d, want 16", got)
	}
	if got := ca.Files["src/my file.rs"]; got != 4 {
		t.Errorf("quoted-path AI lines = %d, want 4 (quotes stripped)", got)
	}
	if len(ca.Agents) != 1 || ca.Agents[0] != "cursor/claude-sonnet-4-5-20250514" {
		t.Errorf("Agents = %v, want [cursor/claude-sonnet-4-5-20250514]", ca.Agents)
	}
}

func TestParseNoteRejectsMalformed(t *testing.T) {
	if _, err := parseNote("no separator here"); err == nil {
		t.Error("expected error for missing --- separator")
	}
	if _, err := parseNote("f.go\n  s_x::t_y 1\n---\nnot json"); err == nil {
		t.Error("expected error for invalid metadata JSON")
	}
	if _, err := parseNote("f.go\n---\n{\"schema_version\":\"other/1.0\"}"); err == nil {
		t.Error("expected error for foreign schema_version")
	}
}

func TestCountLines(t *testing.T) {
	cases := []struct {
		spec string
		want int
	}{
		{"42", 1},
		{"19-222", 204},
		{"1,2,19-222,300", 207}, // 1 + 1 + 204 + 1
		{"", 0},
		{"abc", 0},
		{"5-3", 0}, // inverted range ignored
		{"0-4", 0}, // line numbers are 1-indexed
		{"-3", 0},  // negative / malformed
	}
	for _, tc := range cases {
		if got := countLines(tc.spec); got != tc.want {
			t.Errorf("countLines(%q) = %d, want %d", tc.spec, got, tc.want)
		}
	}
}

func TestSessionLabel(t *testing.T) {
	sessions := map[string]sessionRecord{
		"s_aaa": {AgentID: agentID{Tool: "cursor", Model: "gpt-4"}},
		"s_bbb": {AgentID: agentID{Tool: "claude"}},
	}
	if got := sessionLabel("s_aaa::t_xyz", sessions); got != "cursor/gpt-4" {
		t.Errorf("label = %q", got)
	}
	if got := sessionLabel("s_bbb::t_xyz", sessions); got != "claude" {
		t.Errorf("label = %q", got)
	}
	if got := sessionLabel("0123456789abcdef", sessions); got != "" {
		t.Errorf("legacy key label = %q, want empty", got)
	}
}
