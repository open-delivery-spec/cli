// Package agenttrace reads Agent Trace records (https://agent-trace.dev), the
// open, vendor-neutral format for recording which line ranges of a revision a
// coding agent wrote, with the model and the conversation behind them.
//
// Like git-ai notes, a trace measures authorship line by line instead of
// declaring it once per commit, so when traces cover the change under review
// the AI code ratio is measured, not estimated. Agent Trace defines the
// record, not where it is stored; ODS reads the reference implementation's
// sidecar layout, JSON Lines files under .agent-trace/ at the repository
// root. ODS only consumes traces — recording them is the agent's job.
package agenttrace

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Dir is where the reference implementation keeps trace records.
const Dir = ".agent-trace"

// maxLineBytes bounds one JSONL record; traces with very large files can
// carry many ranges.
const maxLineBytes = 16 << 20

// Record is an Agent Trace record (spec v0.1). Only the fields ODS reads are
// declared; unknown fields are ignored.
type Record struct {
	Version string `json:"version"`
	ID      string `json:"id"`
	VCS     *struct {
		Type     string `json:"type"`
		Revision string `json:"revision"`
	} `json:"vcs"`
	Tool *struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"tool"`
	Files []File `json:"files"`
}

// File is one file's attributed ranges, grouped by conversation.
type File struct {
	Path          string         `json:"path"`
	Conversations []Conversation `json:"conversations"`
}

// Conversation groups the ranges one conversation produced.
type Conversation struct {
	URL         string       `json:"url"`
	Contributor *Contributor `json:"contributor"`
	Ranges      []Range      `json:"ranges"`
}

// Contributor says who wrote a range: human, ai, mixed or unknown.
type Contributor struct {
	Type    string `json:"type"`
	ModelID string `json:"model_id"`
}

// Range is a 1-indexed, inclusive line range at the recorded revision. Its
// own contributor, when present, overrides the conversation's.
type Range struct {
	StartLine   int          `json:"start_line"`
	EndLine     int          `json:"end_line"`
	Contributor *Contributor `json:"contributor"`
}

// CommitAttribution is the AI authorship traces record for one commit.
type CommitAttribution struct {
	Hash    string         // short commit hash
	AILines int            // lines attributed to ai or mixed contributors
	Files   map[string]int // path -> AI lines in that file
	Agents  []string       // "tool/model" labels
}

// RangeAttribution aggregates trace attribution across a commit range.
type RangeAttribution struct {
	Commits []CommitAttribution
	Files   map[string]int // path -> AI lines summed over the range
	Agents  []string       // unique labels, sorted
}

// ReadRange collects trace attribution for the commits in base..HEAD (capped
// at maxCommits; unscoped when the base does not resolve, as the trailer scan
// does). A record counts when its vcs.revision names one of those commits;
// records for other revisions describe other changes. It returns nil with no
// error when no record matches — the common case on repositories that do not
// record traces.
func ReadRange(diffBase string, maxCommits int) (*RangeAttribution, error) {
	dir := traceDir()
	records, err := readRecords(dir)
	if err != nil || len(records) == 0 {
		return nil, err
	}

	args := []string{"log", fmt.Sprintf("-%d", maxCommits), "--format=%H", "--no-merges"}
	if diffBase != "" {
		if _, err := gitOutput("rev-parse", "--verify", diffBase); err == nil {
			args = append(args, diffBase+"..HEAD")
		}
	}
	out, err := gitOutput(args...)
	if err != nil {
		return nil, nil
	}
	return Attribute(records, strings.Fields(out)), nil
}

// Attribute matches records to commits (full SHAs, newest first) and counts
// the AI-attributed lines per file. Pure, so it is testable without git.
// Ranges are merged per commit and file before counting, so two records that
// cover the same lines do not count them twice.
func Attribute(records []Record, commits []string) *RangeAttribution {
	type key struct{ commit, path string }
	ranges := map[key][][2]int{}
	agentsByCommit := map[string]map[string]bool{}

	for _, r := range records {
		if r.VCS == nil || (r.VCS.Type != "" && r.VCS.Type != "git") {
			continue
		}
		commit := matchCommit(r.VCS.Revision, commits)
		if commit == "" {
			continue
		}
		tool := ""
		if r.Tool != nil {
			tool = r.Tool.Name
		}
		for _, f := range r.Files {
			p := filepath.ToSlash(strings.TrimPrefix(f.Path, "./"))
			if p == "" {
				continue
			}
			for _, c := range f.Conversations {
				for _, rg := range c.Ranges {
					who := c.Contributor
					if rg.Contributor != nil {
						who = rg.Contributor
					}
					if !isAI(who) || rg.StartLine < 1 || rg.EndLine < rg.StartLine {
						continue
					}
					k := key{commit, p}
					ranges[k] = append(ranges[k], [2]int{rg.StartLine, rg.EndLine})
					if label := agentLabel(tool, who); label != "" {
						if agentsByCommit[commit] == nil {
							agentsByCommit[commit] = map[string]bool{}
						}
						agentsByCommit[commit][label] = true
					}
				}
			}
		}
	}
	if len(ranges) == 0 {
		return nil
	}

	byCommit := map[string]*CommitAttribution{}
	agg := &RangeAttribution{Files: map[string]int{}}
	for k, rs := range ranges {
		n := mergedLength(rs)
		ca := byCommit[k.commit]
		if ca == nil {
			ca = &CommitAttribution{Hash: shortHash(k.commit), Files: map[string]int{}}
			byCommit[k.commit] = ca
		}
		ca.AILines += n
		ca.Files[k.path] += n
		agg.Files[k.path] += n
	}

	allAgents := map[string]bool{}
	for _, c := range commits { // keep the commit order git gave
		ca := byCommit[c]
		if ca == nil {
			continue
		}
		for a := range agentsByCommit[c] {
			ca.Agents = append(ca.Agents, a)
			allAgents[a] = true
		}
		sort.Strings(ca.Agents)
		agg.Commits = append(agg.Commits, *ca)
	}
	for a := range allAgents {
		agg.Agents = append(agg.Agents, a)
	}
	sort.Strings(agg.Agents)
	return agg
}

// isAI reports whether a contributor counts as AI-assisted. "mixed" is a
// human edit of AI output: still AI-assisted, which is what ODS attributes.
func isAI(c *Contributor) bool {
	if c == nil {
		return false
	}
	switch strings.ToLower(c.Type) {
	case "ai", "mixed":
		return true
	}
	return false
}

func agentLabel(tool string, c *Contributor) string {
	switch {
	case tool != "" && c != nil && c.ModelID != "":
		return tool + "/" + c.ModelID
	case tool != "":
		return tool
	case c != nil && c.ModelID != "":
		return c.ModelID
	}
	return ""
}

// matchCommit resolves a recorded revision to one of the commits: a full SHA
// or an unambiguous prefix of at least seven characters.
func matchCommit(rev string, commits []string) string {
	rev = strings.ToLower(strings.TrimSpace(rev))
	if len(rev) < 7 {
		return ""
	}
	for _, c := range commits {
		if strings.HasPrefix(strings.ToLower(c), rev) {
			return c
		}
	}
	return ""
}

// mergedLength returns the number of distinct lines the ranges cover.
func mergedLength(rs [][2]int) int {
	sort.Slice(rs, func(i, j int) bool { return rs[i][0] < rs[j][0] })
	total, curStart, curEnd := 0, rs[0][0], rs[0][1]
	for _, r := range rs[1:] {
		if r[0] <= curEnd+1 {
			if r[1] > curEnd {
				curEnd = r[1]
			}
			continue
		}
		total += curEnd - curStart + 1
		curStart, curEnd = r[0], r[1]
	}
	return total + curEnd - curStart + 1
}

func shortHash(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// traceDir is .agent-trace at the repository root, or in the working
// directory outside a repository.
func traceDir() string {
	if top, err := gitOutput("rev-parse", "--show-toplevel"); err == nil {
		if t := strings.TrimSpace(top); t != "" {
			return filepath.Join(t, Dir)
		}
	}
	return Dir
}

// readRecords reads every *.jsonl (one record per line) and *.json (one
// record, or an array of records) file in dir. Malformed records are skipped:
// a broken trace must not fail detection.
func readRecords(dir string) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []Record
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		switch filepath.Ext(name) {
		case ".jsonl":
			records = append(records, parseJSONL(data)...)
		case ".json":
			records = append(records, parseJSON(data)...)
		}
	}
	return records, nil
}

func parseJSONL(data []byte) []Record {
	var out []Record
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Record
		if json.Unmarshal(line, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

func parseJSON(data []byte) []Record {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}
	if data[0] == '[' {
		var rs []Record
		if json.Unmarshal(data, &rs) == nil {
			return rs
		}
		return nil
	}
	var r Record
	if json.Unmarshal(data, &r) == nil {
		return []Record{r}
	}
	return nil
}

func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
