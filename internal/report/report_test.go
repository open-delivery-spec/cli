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

func TestRepoNameFromRemote(t *testing.T) {
	cases := map[string]string{
		"https://github.com/open-delivery-spec/cli.git":         "open-delivery-spec/cli",
		"https://github.com/open-delivery-spec/cli":             "open-delivery-spec/cli",
		"https://x-access-token:secret@github.com/org/repo.git": "org/repo", // credentials never reach the report
		"ssh://git@github.com/org/repo.git":                     "org/repo",
		"git@github.com:org/repo.git":                           "org/repo",
		"git@gitlab.example.com:group/subgroup/repo.git":        "subgroup/repo",
		"https://gitlab.example.com/group/subgroup/repo":        "subgroup/repo",
		"/srv/git/org/repo.git":                                 "org/repo",
		"repo":                                                  "repo",
		"":                                                      "",
		"https://github.com":                                    "",
	}
	for in, want := range cases {
		if got := repoNameFromRemote(in); got != want {
			t.Errorf("repoNameFromRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAggregate_recordsBucketGranularity(t *testing.T) {
	short := Aggregate([]Commit{
		{Hash: "a", Date: mustTime("2026-07-06")}, {Hash: "b", Date: mustTime("2026-07-20")},
	}, "30 days ago")
	if short.BucketGranularity != "week" {
		t.Errorf("short span granularity = %q, want week", short.BucketGranularity)
	}
	long := Aggregate([]Commit{
		{Hash: "a", Date: mustTime("2026-01-15")}, {Hash: "b", Date: mustTime("2026-08-20")},
	}, "1 year ago")
	if long.BucketGranularity != "month" {
		t.Errorf("long span granularity = %q, want month", long.BucketGranularity)
	}
	if undated := Aggregate([]Commit{{Hash: "a"}}, "x"); undated.BucketGranularity != "" {
		t.Errorf("undated commits granularity = %q, want empty", undated.BucketGranularity)
	}
}

// TestParseLog_skipsMalformedRecords guards the tolerant parsing of the
// attribution pass: blank records and records without all three fields (hash,
// date, message) are dropped without taking the well-formed ones down with
// them, and empty output yields no commits.
func TestParseLog_skipsMalformedRecords(t *testing.T) {
	const day = "2026-06-01T10:00:00Z"
	attr := recordSep + "only-a-hash" +
		recordSep + "no-message" + fieldSep + day +
		recordSep + "\n" +
		recordSep + "c" + fieldSep + day + fieldSep + "fix: z"

	commits := parseLog(attr, "")
	if len(commits) != 1 || commits[0].Hash != "c" {
		t.Fatalf("parsed %+v, want only commit c", commits)
	}
	if got := parseLog("", ""); len(got) != 0 {
		t.Errorf("empty output parsed to %+v, want no commits", got)
	}
}

// TestParseLog_keepsIncompleteCommits guards what must not be lost: a commit
// whose date does not parse still counts (with a zero date, so it stays out of
// the trend), a commit with an empty message is human rather than dropped, and
// a commit the churn pass did not report has no changed lines.
func TestParseLog_keepsIncompleteCommits(t *testing.T) {
	attr := recordSep + "a" + fieldSep + "not-a-date" + fieldSep +
		"feat: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>" +
		recordSep + "b" + fieldSep + "2026-06-02T10:00:00Z" + fieldSep
	churn := recordSep + "b\n4\t1\tf.go"

	commits := parseLog(attr, churn)
	if len(commits) != 2 {
		t.Fatalf("parsed %d commits, want 2: %+v", len(commits), commits)
	}
	a, b := commits[0], commits[1]
	if !a.Date.IsZero() || a.AITool != "Claude" || a.ChangedLines() != 0 {
		t.Errorf("commit a = %+v, want zero date, Claude, no churn", a)
	}
	if b.IsAI() || b.Date.IsZero() || b.Insertions != 4 || b.Deletions != 1 {
		t.Errorf("commit b = %+v, want human, dated, ins 4, del 1", b)
	}
	if r := Aggregate(commits, "x"); r.TotalCommits != 2 || len(r.Buckets) != 1 || r.Buckets[0].TotalCommits != 1 {
		t.Errorf("aggregate = %d commits / buckets %+v, want both counted but only the dated one in the trend", r.TotalCommits, r.Buckets)
	}
}

// TestParseChurn_ignoresNoiseLines guards the numstat parser against lines
// that are not plain "ins del path" rows: the blank lines git leaves between
// commits, stray single-token lines, and rows whose path holds spaces or is a
// rename must not corrupt the totals.
func TestParseChurn_ignoresNoiseLines(t *testing.T) {
	churn := recordSep + "a\n" +
		"\n" +
		"3\t1\tz.go\n" +
		"stray\n" +
		"\n" +
		"2\t2\tmy file.go\n" +
		"0\t0\told.go => new.go\n"

	if got := parseChurn(churn)["a"]; got != [2]int{5, 3} {
		t.Errorf("churn[a] = %v, want [5 3]", got)
	}
}

// TestParseChurn_recordsEmptyCommitsAndDropsHashless guards the record
// boundaries: a commit with no file changes is present with zero churn, while a
// record whose header line is blank cannot be matched to any commit and is
// dropped rather than filed under the empty hash.
func TestParseChurn_recordsEmptyCommitsAndDropsHashless(t *testing.T) {
	m := parseChurn(recordSep + "a\n" + recordSep + "b\n1\t1\tx.go")
	if got, ok := m["a"]; !ok || got != [2]int{0, 0} {
		t.Errorf("churn[a] = %v (present: %v), want a zero entry", got, ok)
	}
	if got := m["b"]; got != [2]int{1, 1} {
		t.Errorf("churn[b] = %v, want [1 1]", got)
	}

	if got := parseChurn(recordSep + "\n3\t1\tz.go"); len(got) != 0 {
		t.Errorf("hashless record produced %v, want nothing", got)
	}
}

// TestBuildBuckets_weekBoundaries guards the ISO week key: weeks run Monday to
// Sunday, so a Sunday belongs to the week that began six days earlier, and
// around New Year the label carries the ISO year, not the calendar year.
func TestBuildBuckets_weekBoundaries(t *testing.T) {
	cases := []struct{ date, label, start string }{
		{"2026-07-06", "2026-W28", "2026-07-06"}, // Monday opens its week
		{"2026-07-12", "2026-W28", "2026-07-06"}, // Sunday closes it
		{"2026-07-13", "2026-W29", "2026-07-13"}, // the next Monday opens the next
		{"2026-01-01", "2026-W01", "2025-12-29"}, // week 1 starts in the previous calendar year
		{"2027-01-01", "2026-W53", "2026-12-28"}, // 2026 has a week 53 that runs into 2027
	}
	for _, tc := range cases {
		t.Run(tc.date, func(t *testing.T) {
			buckets, granularity := buildBuckets([]Commit{{Hash: "x", Date: mustTime(tc.date)}})
			if granularity != "week" || len(buckets) != 1 {
				t.Fatalf("got %q granularity and buckets %+v, want one weekly bucket", granularity, buckets)
			}
			if buckets[0].Label != tc.label || buckets[0].Start != tc.start {
				t.Errorf("bucket = %q starting %q, want %q starting %q", buckets[0].Label, buckets[0].Start, tc.label, tc.start)
			}
		})
	}
}

// TestBuildBuckets_switchesToMonthsAfterTwentySixWeeks guards the granularity
// threshold: a span of exactly 26 weeks stays weekly, anything longer goes
// monthly.
func TestBuildBuckets_switchesToMonthsAfterTwentySixWeeks(t *testing.T) {
	first := mustTime("2026-01-05")
	exactly := mustTime("2026-07-06") // 182 days, 26 weeks, later

	if _, g := buildBuckets([]Commit{{Date: first}, {Date: exactly}}); g != "week" {
		t.Errorf("26 weeks: granularity = %q, want week", g)
	}
	if _, g := buildBuckets([]Commit{{Date: first}, {Date: exactly.Add(time.Second)}}); g != "month" {
		t.Errorf("26 weeks and a second: granularity = %q, want month", g)
	}
}

// TestBuildBuckets_sortsNewestFirstInput guards the ordering git hands over:
// git log lists newest first, and the trend still has to read oldest to newest.
func TestBuildBuckets_sortsNewestFirstInput(t *testing.T) {
	buckets, _ := buildBuckets([]Commit{
		{Hash: "c", AITool: "Claude", Date: mustTime("2026-07-14")},
		{Hash: "b", Date: mustTime("2026-07-07")},
		{Hash: "a", Date: mustTime("2026-07-06")},
	})
	if len(buckets) != 2 || buckets[0].Label != "2026-W28" || buckets[1].Label != "2026-W29" {
		t.Fatalf("buckets = %+v, want W28 then W29", buckets)
	}
	if buckets[0].TotalCommits != 2 || buckets[1].TotalCommits != 1 || buckets[1].AIShare != 1 {
		t.Errorf("buckets = %+v, want 2 commits in W28 and 1 AI commit in W29", buckets)
	}
}
