package agenttrace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	shaA = "1111111111111111111111111111111111111111"
	shaB = "2222222222222222222222222222222222222222"
)

// record is a compact Agent Trace record builder for the tests.
func record(vcsType, rev, tool string, files ...File) Record {
	r := Record{Version: "0.1.0", ID: "r", Files: files}
	r.VCS = &struct {
		Type     string `json:"type"`
		Revision string `json:"revision"`
	}{vcsType, rev}
	if tool != "" {
		r.Tool = &struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}{Name: tool}
	}
	return r
}

func conv(typ, model string, ranges ...Range) Conversation {
	return Conversation{Contributor: &Contributor{Type: typ, ModelID: model}, Ranges: ranges}
}

func TestAttribute(t *testing.T) {
	commits := []string{shaB, shaA} // newest first, as git log gives them

	t.Run("counts ai and mixed ranges of matching commits", func(t *testing.T) {
		recs := []Record{
			record("git", shaA, "claude-code",
				File{Path: "svc/a.go", Conversations: []Conversation{
					conv("ai", "anthropic/claude-opus-4-8", Range{StartLine: 1, EndLine: 10}),
					conv("human", "", Range{StartLine: 11, EndLine: 20}),
				}},
				File{Path: "./svc/b.go", Conversations: []Conversation{
					conv("mixed", "", Range{StartLine: 5, EndLine: 6}),
				}},
			),
			record("git", shaB[:7], "cursor",
				File{Path: "svc/a.go", Conversations: []Conversation{
					conv("ai", "openai/gpt-5", Range{StartLine: 30, EndLine: 31}),
				}},
			),
		}
		got := Attribute(recs, commits)
		if got == nil {
			t.Fatal("Attribute = nil, want attribution")
		}
		if want := map[string]int{"svc/a.go": 12, "svc/b.go": 2}; !reflect.DeepEqual(got.Files, want) {
			t.Errorf("Files = %v, want %v", got.Files, want)
		}
		if len(got.Commits) != 2 || got.Commits[0].Hash != shaB[:7] || got.Commits[1].Hash != shaA[:7] {
			t.Fatalf("Commits = %+v, want B then A", got.Commits)
		}
		if got.Commits[1].AILines != 12 {
			t.Errorf("commit A AILines = %d, want 12", got.Commits[1].AILines)
		}
		wantAgents := []string{"claude-code", "claude-code/anthropic/claude-opus-4-8", "cursor/openai/gpt-5"}
		if !reflect.DeepEqual(got.Agents, wantAgents) {
			t.Errorf("Agents = %v, want %v", got.Agents, wantAgents)
		}
	})

	t.Run("overlapping ranges count once", func(t *testing.T) {
		recs := []Record{
			record("git", shaA, "", File{Path: "a.go", Conversations: []Conversation{
				conv("ai", "", Range{StartLine: 1, EndLine: 10}, Range{StartLine: 5, EndLine: 12}, Range{StartLine: 13, EndLine: 13}),
			}}),
			record("git", shaA, "", File{Path: "a.go", Conversations: []Conversation{
				conv("ai", "", Range{StartLine: 20, EndLine: 21}, Range{StartLine: 2, EndLine: 3}),
			}}),
		}
		got := Attribute(recs, commits)
		if got == nil || got.Files["a.go"] != 15 {
			t.Errorf("Files = %+v, want a.go: 15 (1-13 and 20-21)", got)
		}
	})

	t.Run("a range's own contributor overrides the conversation's", func(t *testing.T) {
		human := &Contributor{Type: "human"}
		ai := &Contributor{Type: "AI"}
		recs := []Record{record("git", shaA, "", File{Path: "a.go", Conversations: []Conversation{
			conv("ai", "", Range{StartLine: 1, EndLine: 4, Contributor: human}, Range{StartLine: 5, EndLine: 5}),
			{Ranges: []Range{{StartLine: 9, EndLine: 9, Contributor: ai}, {StartLine: 10, EndLine: 10}}},
		}})}
		got := Attribute(recs, commits)
		if got == nil || got.Files["a.go"] != 2 {
			t.Errorf("Files = %+v, want a.go: 2 (line 5 and line 9)", got)
		}
	})

	t.Run("records outside the change and malformed ranges count nothing", func(t *testing.T) {
		recs := []Record{
			record("git", "3333333333333333333333333333333333333333", "", File{Path: "a.go", Conversations: []Conversation{conv("ai", "", Range{StartLine: 1, EndLine: 9})}}),
			record("git", "1111", "", File{Path: "a.go", Conversations: []Conversation{conv("ai", "", Range{StartLine: 1, EndLine: 9})}}),
			record("jj", shaA, "", File{Path: "a.go", Conversations: []Conversation{conv("ai", "", Range{StartLine: 1, EndLine: 9})}}),
			record("git", shaA, "", File{Path: "a.go", Conversations: []Conversation{conv("ai", "", Range{StartLine: 0, EndLine: 9}, Range{StartLine: 9, EndLine: 3})}}),
			record("git", shaA, "", File{Path: "", Conversations: []Conversation{conv("ai", "", Range{StartLine: 1, EndLine: 9})}}),
			record("git", shaA, "", File{Path: "a.go", Conversations: []Conversation{conv("unknown", "", Range{StartLine: 1, EndLine: 9})}}),
			{Version: "0.1.0", Files: []File{{Path: "a.go"}}}, // no vcs
		}
		if got := Attribute(recs, commits); got != nil {
			t.Errorf("Attribute = %+v, want nil", got)
		}
	})

	t.Run("an absent vcs type means git", func(t *testing.T) {
		recs := []Record{record("", shaA, "", File{Path: "a.go", Conversations: []Conversation{conv("ai", "", Range{StartLine: 1, EndLine: 2})}})}
		if got := Attribute(recs, commits); got == nil || got.Files["a.go"] != 2 {
			t.Errorf("Attribute = %+v, want a.go: 2", got)
		}
	})
}

func TestReadRecords(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("traces.jsonl", `{"version":"0.1.0","id":"1","vcs":{"type":"git","revision":"`+shaA+`"},"files":[]}

not json
{"version":"0.1.0","id":"2","files":[]}
`)
	write("one.json", `{"version":"0.1.0","id":"3","files":[]}`)
	write("many.json", `[{"id":"4","files":[]},{"id":"5","files":[]}]`)
	write("broken.json", `{`)
	write("notes.txt", `{"id":"6"}`)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	recs, err := readRecords(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range recs {
		ids = append(ids, r.ID)
	}
	// os.ReadDir sorts by name: broken.json, many.json, notes.txt, one.json, traces.jsonl
	if want := []string{"4", "5", "3", "1", "2"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("record ids = %v, want %v", ids, want)
	}
	if recs[3].VCS == nil || recs[3].VCS.Revision != shaA {
		t.Errorf("vcs not parsed: %+v", recs[3].VCS)
	}

	if recs, err := readRecords(filepath.Join(dir, "missing")); err != nil || recs != nil {
		t.Errorf("missing dir = %v, %v; want nil, nil", recs, err)
	}
	if got := parseJSON([]byte("  ")); got != nil {
		t.Errorf("parseJSON(blank) = %v", got)
	}
	if got := parseJSON([]byte("[1]")); got != nil {
		t.Errorf("parseJSON(bad array) = %v", got)
	}
}

func TestHelpers(t *testing.T) {
	if got := mergedLength([][2]int{{5, 6}, {1, 2}, {3, 4}}); got != 6 {
		t.Errorf("adjacent ranges = %d, want 6", got)
	}
	if got := mergedLength([][2]int{{1, 10}, {2, 3}}); got != 10 {
		t.Errorf("contained range = %d, want 10", got)
	}
	if got := matchCommit("ABCDEF1", []string{"abcdef1234"}); got != "abcdef1234" {
		t.Errorf("matchCommit is case-insensitive, got %q", got)
	}
	if got := shortHash("abc"); got != "abc" {
		t.Errorf("shortHash = %q", got)
	}
	if got := agentLabel("", &Contributor{ModelID: "m"}); got != "m" {
		t.Errorf("agentLabel = %q", got)
	}
	if got := agentLabel("", nil); got != "" {
		t.Errorf("agentLabel = %q", got)
	}
}
