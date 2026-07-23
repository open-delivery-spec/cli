package report

import (
	"strings"
	"testing"
	"time"
)

func mustTime(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBuildBuckets_weekly(t *testing.T) {
	commits := []Commit{
		{Hash: "a", AITool: "Claude", Date: mustTime("2026-07-06")}, // W28 (Mon)
		{Hash: "b", AITool: "", Date: mustTime("2026-07-08")},       // W28, human
		{Hash: "c", AITool: "Cursor", Date: mustTime("2026-07-14")}, // W29
	}
	r := Aggregate(commits, "30 days ago")
	if len(r.Buckets) != 2 {
		t.Fatalf("buckets = %d, want 2 (%+v)", len(r.Buckets), r.Buckets)
	}
	// Chronological order.
	if r.Buckets[0].Start > r.Buckets[1].Start {
		t.Errorf("buckets not sorted: %+v", r.Buckets)
	}
	w28 := r.Buckets[0]
	if w28.TotalCommits != 2 || w28.AICommits != 1 || w28.AIShare < 0.49 || w28.AIShare > 0.51 {
		t.Errorf("W28 = %+v, want 2 commits / 1 AI / ~0.5 share", w28)
	}
}

func TestBuildBuckets_skipsUndatedCommits(t *testing.T) {
	// The other Aggregate tests use undated commits — those must yield no buckets,
	// never a panic or a zero-time slice.
	r := Aggregate([]Commit{{AITool: "Claude"}, {AITool: ""}}, "x")
	if len(r.Buckets) != 0 {
		t.Errorf("undated commits should produce no buckets, got %+v", r.Buckets)
	}
}

func TestBuildBuckets_monthlyForLongSpan(t *testing.T) {
	commits := []Commit{
		{Hash: "a", AITool: "Claude", Date: mustTime("2026-01-15")},
		{Hash: "b", AITool: "", Date: mustTime("2026-08-20")}, // > 26 weeks later
	}
	r := Aggregate(commits, "1 year ago")
	for _, bk := range r.Buckets {
		if strings.Contains(bk.Label, "-W") {
			t.Errorf("long span should bucket monthly, got weekly label %q", bk.Label)
		}
	}
	if len(r.Buckets) != 2 {
		t.Errorf("monthly buckets = %d, want 2 (%+v)", len(r.Buckets), r.Buckets)
	}
}

func TestRenderHTML_containsHeadlineNumbers(t *testing.T) {
	commits := []Commit{
		{Hash: "a", AITool: "Claude", Insertions: 100, Date: mustTime("2026-07-06")},
		{Hash: "b", AITool: "", Insertions: 100, Date: mustTime("2026-07-08")},
	}
	r := Aggregate(commits, "30 days ago")
	doc := RenderHTML(r, mustTime("2026-07-16"))
	for _, want := range []string{"<!DOCTYPE html>", "AI Attribution Report", "50%", "<svg", "Claude", "</html>"} {
		if !strings.Contains(doc, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestRenderHTML_emptyWindow(t *testing.T) {
	doc := RenderHTML(Aggregate(nil, "30 days ago"), time.Now())
	if !strings.Contains(doc, "No commits in the selected window") {
		t.Error("empty report should say so")
	}
	if strings.Contains(doc, "<svg") {
		t.Error("empty report should not draw a chart")
	}
}

func TestRenderHTML_escapesToolName(t *testing.T) {
	commits := []Commit{{Hash: "a", AITool: "<script>x</script>", Date: mustTime("2026-07-06")}}
	doc := RenderHTML(Aggregate(commits, "x"), time.Now())
	if strings.Contains(doc, "<script>x</script>") {
		t.Error("tool name must be HTML-escaped")
	}
}

func TestAggregate(t *testing.T) {
	commits := []Commit{
		{Hash: "a", AITool: "Claude", Insertions: 100, Deletions: 0},         // AI, 100 lines
		{Hash: "b", AITool: "", Insertions: 50, Deletions: 50},               // human, 100 lines
		{Hash: "c", AITool: "GitHub Copilot", Insertions: 10, Deletions: 10}, // AI, 20 lines
	}
	r := Aggregate(commits, "90 days ago")

	if r.TotalCommits != 3 || r.AICommits != 2 || r.HumanCommits != 1 {
		t.Fatalf("counts = total %d ai %d human %d, want 3/2/1", r.TotalCommits, r.AICommits, r.HumanCommits)
	}
	if r.TotalChangedLines != 220 || r.AIChangedLines != 120 {
		t.Errorf("lines = total %d ai %d, want 220/120", r.TotalChangedLines, r.AIChangedLines)
	}
	if got := r.AICommitShare; got < 0.666 || got > 0.667 {
		t.Errorf("AICommitShare = %f, want ~0.667", got)
	}
	if got := r.AILineShare; got < 0.545 || got > 0.546 {
		t.Errorf("AILineShare = %f, want ~0.545", got)
	}
	if r.ByTool["Claude"] != 1 || r.ByTool["GitHub Copilot"] != 1 {
		t.Errorf("ByTool = %v, want Claude:1 Copilot:1", r.ByTool)
	}
	if r.Summary == "" {
		t.Error("summary should not be empty")
	}
}

func TestAggregate_empty(t *testing.T) {
	r := Aggregate(nil, "30 days ago")
	if r.TotalCommits != 0 || r.AICommitShare != 0 || r.AILineShare != 0 {
		t.Errorf("empty aggregate non-zero: %+v", r)
	}
	if r.Summary != "No commits in the selected window" {
		t.Errorf("summary = %q", r.Summary)
	}
}

func TestToolBreakdown_sorted(t *testing.T) {
	r := Aggregate([]Commit{
		{AITool: "Claude"}, {AITool: "Claude"}, {AITool: "Cursor"},
	}, "x")
	b := r.ToolBreakdown()
	if len(b) != 2 || b[0].Tool != "Claude" || b[0].Commits != 2 || b[1].Tool != "Cursor" {
		t.Errorf("breakdown = %+v, want Claude(2) then Cursor(1)", b)
	}
}

func TestParseLog(t *testing.T) {
	attr := recordSep + "a" + fieldSep + "2026-06-01T10:00:00Z" + fieldSep +
		"feat: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>" +
		recordSep + "b" + fieldSep + "2026-06-02T10:00:00Z" + fieldSep + "fix: y"
	churn := recordSep + "a\n10\t2\tfile.go\n5\t0\tx.go" +
		recordSep + "b\n3\t1\tz.go"

	commits := parseLog(attr, churn)
	if len(commits) != 2 {
		t.Fatalf("parsed %d commits, want 2", len(commits))
	}
	a := commits[0]
	if a.Hash != "a" || a.AITool != "Claude" || a.Insertions != 15 || a.Deletions != 2 {
		t.Errorf("commit a = %+v, want hash a, Claude, ins 15, del 2", a)
	}
	if a.Date.Year() != 2026 || a.Date.Month() != 6 {
		t.Errorf("commit a date = %v, want June 2026", a.Date)
	}
	b := commits[1]
	if b.Hash != "b" || b.IsAI() || b.Insertions != 3 || b.Deletions != 1 {
		t.Errorf("commit b = %+v, want hash b, human, ins 3, del 1", b)
	}
}

func TestParseChurn_skipsBinary(t *testing.T) {
	// Binary files report "-" for ins/del and must be ignored.
	churn := recordSep + "a\n-\t-\timage.png\n7\t3\tcode.go"
	m := parseChurn(churn)
	if m["a"] != [2]int{7, 3} {
		t.Errorf("churn[a] = %v, want [7 3]", m["a"])
	}
}
