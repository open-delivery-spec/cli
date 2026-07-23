// Package report builds AI-attribution analytics over recent git history.
//
// Unlike the per-PR pipeline, this answers a governance question: across a
// window of merged work, how much of it is AI-assisted and trending which way?
// Attribution comes from the `Co-Authored-By` trailers AI tools emit — the same
// signal the detector uses — so the report reflects what the tools disclose, not
// forensic detection.
package report

import (
	"bufio"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/open-delivery-spec/cli/internal/detector"
)

// Record separators chosen to not collide with commit content.
const (
	recordSep = "\x1e"
	fieldSep  = "\x1f"
)

// Commit is a single non-merge commit with its AI attribution and churn.
type Commit struct {
	Hash       string    `json:"hash"`
	Date       time.Time `json:"date"`
	AITool     string    `json:"ai_tool,omitempty"` // "" when human-authored
	Insertions int       `json:"insertions"`
	Deletions  int       `json:"deletions"`
}

// IsAI reports whether the commit carries AI attribution.
func (c Commit) IsAI() bool { return c.AITool != "" }

// ChangedLines is insertions + deletions.
func (c Commit) ChangedLines() int { return c.Insertions + c.Deletions }

// Report is the aggregated AI-attribution summary over a window.
type Report struct {
	Since             string         `json:"since"`
	TotalCommits      int            `json:"total_commits"`
	AICommits         int            `json:"ai_commits"`
	HumanCommits      int            `json:"human_commits"`
	AICommitShare     float64        `json:"ai_commit_share"` // 0..1
	TotalChangedLines int            `json:"total_changed_lines"`
	AIChangedLines    int            `json:"ai_changed_lines"`
	AILineShare       float64        `json:"ai_line_share"` // 0..1
	ByTool            map[string]int `json:"by_tool"`       // tool -> commit count
	Summary           string         `json:"summary"`
	// Buckets is a chronological time series (weekly, or monthly for long
	// windows) of AI vs human activity — the "trending which way" view. Empty
	// when commits carry no dates.
	Buckets []TimeBucket `json:"buckets,omitempty"`
}

// TimeBucket aggregates one time slice of history for the trend view.
type TimeBucket struct {
	Label        string  `json:"label"` // e.g. "2026-W28" or "2026-07"
	Start        string  `json:"start"` // ISO date of the slice start
	TotalCommits int     `json:"total_commits"`
	AICommits    int     `json:"ai_commits"`
	AIShare      float64 `json:"ai_share"` // 0..1 of commits in this slice
}

// Options configures history collection.
type Options struct {
	// Since is any git --since expression (e.g. "90 days ago", "2026-01-01").
	Since string
	// MaxCommits caps the number of commits scanned (0 = no cap).
	MaxCommits int
}

// Collect gathers commits from git history and aggregates them.
func Collect(opts Options) (*Report, error) {
	if opts.Since == "" {
		opts.Since = "90 days ago"
	}
	commits, err := collectCommits(opts)
	if err != nil {
		return nil, err
	}
	r := Aggregate(commits, opts.Since)
	return &r, nil
}

// collectCommits runs git and parses attribution + churn into commits.
func collectCommits(opts Options) ([]Commit, error) {
	args := []string{"log", "--no-merges", "--since=" + opts.Since}
	if opts.MaxCommits > 0 {
		args = append(args, fmt.Sprintf("-%d", opts.MaxCommits))
	}

	attrArgs := append(append([]string{}, args...),
		"--pretty=format:"+recordSep+"%H"+fieldSep+"%cI"+fieldSep+"%B")
	attrOut, err := gitOutput(attrArgs...)
	if err != nil {
		return nil, fmt.Errorf("reading git history: %w", err)
	}

	churnArgs := append(append([]string{}, args...), "--numstat",
		"--pretty=format:"+recordSep+"%H")
	churnOut, err := gitOutput(churnArgs...)
	if err != nil {
		return nil, fmt.Errorf("reading git churn: %w", err)
	}

	return parseLog(attrOut, churnOut), nil
}

// parseLog combines the attribution and churn git outputs into commits.
// Exposed (unexported) for testing without invoking git.
func parseLog(attrOut, churnOut string) []Commit {
	churn := parseChurn(churnOut)

	var commits []Commit
	for _, rec := range strings.Split(attrOut, recordSep) {
		rec = strings.TrimLeft(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		parts := strings.SplitN(rec, fieldSep, 3)
		if len(parts) < 3 {
			continue
		}
		hash := strings.TrimSpace(parts[0])
		date, _ := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
		body := parts[2]

		ins, del := churn[hash][0], churn[hash][1]
		commits = append(commits, Commit{
			Hash:       hash,
			Date:       date,
			AITool:     detector.AITrailerTool(body),
			Insertions: ins,
			Deletions:  del,
		})
	}
	return commits
}

// parseChurn maps commit hash -> [insertions, deletions] from `--numstat` output.
func parseChurn(churnOut string) map[string][2]int {
	out := make(map[string][2]int)
	for _, rec := range strings.Split(churnOut, recordSep) {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(rec))
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		hash := ""
		var ins, del int
		first := true
		for scanner.Scan() {
			line := strings.TrimRight(scanner.Text(), "\n")
			if first {
				hash = strings.TrimSpace(line)
				first = false
				continue
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			// Binary files report "-" for ins/del; skip them.
			a, errA := strconv.Atoi(fields[0])
			b, errB := strconv.Atoi(fields[1])
			if errA == nil {
				ins += a
			}
			if errB == nil {
				del += b
			}
		}
		if hash != "" {
			out[hash] = [2]int{ins, del}
		}
	}
	return out
}

// Aggregate computes the report from a set of commits. Pure and deterministic.
func Aggregate(commits []Commit, since string) Report {
	r := Report{Since: since, ByTool: map[string]int{}}
	for _, c := range commits {
		r.TotalCommits++
		r.TotalChangedLines += c.ChangedLines()
		if c.IsAI() {
			r.AICommits++
			r.AIChangedLines += c.ChangedLines()
			r.ByTool[c.AITool]++
		} else {
			r.HumanCommits++
		}
	}
	if r.TotalCommits > 0 {
		r.AICommitShare = float64(r.AICommits) / float64(r.TotalCommits)
	}
	if r.TotalChangedLines > 0 {
		r.AILineShare = float64(r.AIChangedLines) / float64(r.TotalChangedLines)
	}
	r.Summary = summarize(r)
	r.Buckets = buildBuckets(commits)
	return r
}

// buildBuckets groups dated commits into a chronological trend. Commits with a
// zero date are skipped (git always provides dates; test fixtures may not).
// Granularity is weekly, or monthly when the span exceeds ~26 weeks, so a
// year-long window stays readable.
func buildBuckets(commits []Commit) []TimeBucket {
	var dated []Commit
	var min, max time.Time
	for _, c := range commits {
		if c.Date.IsZero() {
			continue
		}
		dated = append(dated, c)
		if min.IsZero() || c.Date.Before(min) {
			min = c.Date
		}
		if c.Date.After(max) {
			max = c.Date
		}
	}
	if len(dated) == 0 {
		return nil
	}

	monthly := max.Sub(min) > 26*7*24*time.Hour
	key := func(t time.Time) (label, start string) {
		if monthly {
			return t.Format("2006-01"), t.Format("2006-01") + "-01"
		}
		// ISO week label; slice start is the Monday of that week.
		y, w := t.ISOWeek()
		weekday := (int(t.Weekday()) + 6) % 7 // Monday=0
		monday := t.AddDate(0, 0, -weekday)
		return fmt.Sprintf("%d-W%02d", y, w), monday.Format("2006-01-02")
	}

	idx := map[string]int{}
	var out []TimeBucket
	for _, c := range dated {
		label, start := key(c.Date)
		i, ok := idx[label]
		if !ok {
			i = len(out)
			idx[label] = i
			out = append(out, TimeBucket{Label: label, Start: start})
		}
		out[i].TotalCommits++
		if c.IsAI() {
			out[i].AICommits++
		}
	}
	for i := range out {
		if out[i].TotalCommits > 0 {
			out[i].AIShare = float64(out[i].AICommits) / float64(out[i].TotalCommits)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func summarize(r Report) string {
	if r.TotalCommits == 0 {
		return "No commits in the selected window"
	}
	return fmt.Sprintf(
		"%d commit(s): %d AI-assisted (%.0f%%), %d human — AI touched %.0f%% of changed lines",
		r.TotalCommits, r.AICommits, r.AICommitShare*100, r.HumanCommits, r.AILineShare*100,
	)
}

// ToolBreakdown returns the per-tool commit counts sorted by count desc, then name.
func (r Report) ToolBreakdown() []ToolCount {
	out := make([]ToolCount, 0, len(r.ByTool))
	for tool, n := range r.ByTool {
		out = append(out, ToolCount{Tool: tool, Commits: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Commits != out[j].Commits {
			return out[i].Commits > out[j].Commits
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

// ToolCount is one row of the per-tool breakdown.
type ToolCount struct {
	Tool    string `json:"tool"`
	Commits int    `json:"commits"`
}

// gitOutput runs a git command and returns its stdout (not trimmed, to preserve
// record/field separators and trailing content).
func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
