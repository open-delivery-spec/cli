// Package gitai reads line-level AI authorship recorded by git-ai
// (https://github.com/git-ai-project/git-ai) under the refs/notes/ai
// namespace, following the Git AI Standard v3 authorship-log format.
//
// Where commit trailers disclose that a change was AI-assisted, git-ai
// records *which lines* each agent wrote. When these notes are present they
// are the highest-fidelity attribution signal ODS can read: the AI code
// ratio becomes a measured value instead of a confidence-based estimate.
// ODS only consumes the notes — capturing them is git-ai's job.
package gitai

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// NotesRef is the notes namespace mandated by the Git AI Standard.
const NotesRef = "ai"

// CommitAttribution is the parsed authorship log of one commit.
type CommitAttribution struct {
	Hash       string         // short commit hash
	AILines    int            // lines attributed to AI sessions (incl. legacy keys)
	HumanLines int            // lines explicitly attributed to humans
	Files      map[string]int // path -> AI lines in that file
	Agents     []string       // display labels, e.g. "cursor/claude-sonnet-4-5"
}

// RangeAttribution aggregates authorship logs across a commit range.
type RangeAttribution struct {
	Commits []CommitAttribution
	Files   map[string]int // path -> AI lines summed over the range
	Agents  []string       // unique display labels, sorted
}

// ReadRange collects git-ai authorship for the commits in base..HEAD (capped
// at maxCommits; unscoped when the base does not resolve, mirroring the
// trailer scan). It returns nil with no error when no commit in the range
// carries an AI note — the common case on repos not using git-ai.
func ReadRange(diffBase string, maxCommits int) (*RangeAttribution, error) {
	args := []string{"log", fmt.Sprintf("-%d", maxCommits), "--format=%H", "--no-merges"}
	if diffBase != "" {
		if _, err := gitOutput("rev-parse", "--verify", diffBase); err == nil {
			args = append(args, diffBase+"..HEAD")
		}
	}
	out, err := gitOutput(args...)
	if err != nil || strings.TrimSpace(out) == "" {
		return nil, nil
	}

	agg := &RangeAttribution{Files: map[string]int{}}
	agents := map[string]bool{}
	for _, sha := range strings.Fields(out) {
		note, err := gitOutput("notes", "--ref="+NotesRef, "show", sha)
		if err != nil || strings.TrimSpace(note) == "" {
			continue // no note on this commit
		}
		ca, err := parseNote(note)
		if err != nil {
			continue // tolerate malformed notes rather than failing detection
		}
		short, _ := gitOutput("rev-parse", "--short", sha)
		ca.Hash = strings.TrimSpace(short)
		agg.Commits = append(agg.Commits, *ca)
		for path, n := range ca.Files {
			agg.Files[path] += n
		}
		for _, a := range ca.Agents {
			agents[a] = true
		}
	}
	if len(agg.Commits) == 0 {
		return nil, nil
	}
	for a := range agents {
		agg.Agents = append(agg.Agents, a)
	}
	sort.Strings(agg.Agents)
	return agg, nil
}

// ── Authorship-log parsing (Git AI Standard v3, section 1.2) ────────────────

// metadata is the JSON section below the "---" separator.
type metadata struct {
	SchemaVersion string                   `json:"schema_version"`
	Sessions      map[string]sessionRecord `json:"sessions"`
}

type sessionRecord struct {
	AgentID agentID `json:"agent_id"`
}

type agentID struct {
	Tool  string `json:"tool"`
	Model string `json:"model"`
}

var legacyKeyPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// parseNote parses one authorship log: an attestation section (file paths at
// column 0, entries indented two spaces as "<key> <ranges>") separated from a
// JSON metadata section by a line containing exactly "---".
func parseNote(note string) (*CommitAttribution, error) {
	lines := strings.Split(note, "\n")
	sep := -1
	for i, l := range lines {
		if l == "---" {
			sep = i
			break
		}
	}
	if sep == -1 {
		return nil, fmt.Errorf("authorship log missing --- separator")
	}

	var meta metadata
	if err := json.Unmarshal([]byte(strings.Join(lines[sep+1:], "\n")), &meta); err != nil {
		return nil, fmt.Errorf("parsing authorship metadata: %w", err)
	}
	if !strings.HasPrefix(meta.SchemaVersion, "authorship/") {
		return nil, fmt.Errorf("unrecognized schema_version %q", meta.SchemaVersion)
	}

	ca := &CommitAttribution{Files: map[string]int{}}
	agents := map[string]bool{}
	currentFile := ""
	for _, raw := range lines[:sep] {
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "  ") {
			// Attribution entry: "<key> <line-ranges>"
			fields := strings.Fields(raw)
			if len(fields) < 2 || currentFile == "" {
				continue
			}
			key, ranges := fields[0], fields[1]
			n := countLines(ranges)
			switch {
			case strings.HasPrefix(key, "h_"):
				ca.HumanLines += n
			case strings.Contains(key, "::") || strings.HasPrefix(key, "s_") || legacyKeyPattern.MatchString(key):
				ca.AILines += n
				ca.Files[currentFile] += n
				if label := sessionLabel(key, meta.Sessions); label != "" {
					agents[label] = true
				}
			}
		} else {
			// File path line; quoted when it contains whitespace.
			currentFile = strings.Trim(strings.TrimSpace(raw), `"`)
		}
	}
	for a := range agents {
		ca.Agents = append(ca.Agents, a)
	}
	sort.Strings(ca.Agents)
	return ca, nil
}

// sessionLabel resolves an attestation key to a "tool/model" display label
// via the sessions map ("" when unresolvable, e.g. legacy keys).
func sessionLabel(key string, sessions map[string]sessionRecord) string {
	sid, _, _ := strings.Cut(key, "::")
	rec, ok := sessions[sid]
	if !ok {
		return ""
	}
	switch {
	case rec.AgentID.Tool != "" && rec.AgentID.Model != "":
		return rec.AgentID.Tool + "/" + rec.AgentID.Model
	case rec.AgentID.Tool != "":
		return rec.AgentID.Tool
	default:
		return ""
	}
}

// countLines evaluates a line-range specification ("42", "19-222",
// "1,2,19-222,300") to a line count. Malformed pieces count zero.
func countLines(spec string) int {
	total := 0
	for _, part := range strings.Split(spec, ",") {
		if a, b, ok := strings.Cut(part, "-"); ok {
			start, err1 := strconv.Atoi(a)
			end, err2 := strconv.Atoi(b)
			if err1 == nil && err2 == nil && end >= start && start > 0 {
				total += end - start + 1
			}
		} else if n, err := strconv.Atoi(part); err == nil && n > 0 {
			total++
		}
	}
	return total
}

func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
