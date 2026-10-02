package report

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func repoReport(repo string, commits []Commit) Report {
	r := Aggregate(commits, "90 days ago")
	r.Repo = repo
	return r
}

func TestMerge_sumsAcrossRepositories(t *testing.T) {
	a := repoReport("org/a", []Commit{
		{Hash: "1", AITool: "Claude", Insertions: 100, Deletions: 10, Date: mustTime("2026-07-06")},
		{Hash: "2", AITool: "Claude", Insertions: 50, Date: mustTime("2026-07-07")},
	})
	b := repoReport("org/b", []Commit{
		{Hash: "3", AITool: "GitHub Copilot", Insertions: 20, Date: mustTime("2026-07-08")},
		{Hash: "4", AITool: "Claude", Insertions: 20, Date: mustTime("2026-07-14")},
		{Hash: "5", AITool: "", Insertions: 5, Deletions: 5, Date: mustTime("2026-07-15")},
		{Hash: "6", AITool: "", Insertions: 1, Date: mustTime("2026-07-16")},
	})
	c := repoReport("org/c", nil) // scanned, nothing in the window

	o := Merge([]Report{a, b, c})

	if o.ReposCovered != 3 || o.ReposWithAI != 2 {
		t.Errorf("repos covered/with AI = %d/%d, want 3/2", o.ReposCovered, o.ReposWithAI)
	}
	if o.TotalCommits != 6 || o.AICommits != 4 || o.HumanCommits != 2 {
		t.Errorf("commits = %d total / %d AI / %d human, want 6/4/2", o.TotalCommits, o.AICommits, o.HumanCommits)
	}
	if math.Abs(o.AICommitShare-2.0/3) > 1e-9 {
		t.Errorf("AI commit share = %v, want 2/3", o.AICommitShare)
	}
	if o.TotalChangedLines != 211 || o.AIChangedLines != 200 {
		t.Errorf("changed lines = %d total / %d AI, want 211/200", o.TotalChangedLines, o.AIChangedLines)
	}
	if o.ByTool["Claude"] != 3 || o.ByTool["GitHub Copilot"] != 1 {
		t.Errorf("by_tool = %v, want Claude 3, GitHub Copilot 1", o.ByTool)
	}
	if o.Since != "90 days ago" {
		t.Errorf("since = %q, want the shared window", o.Since)
	}
	// Most AI-heavy repository first (a: 100%, b: 50%); the empty one last.
	if len(o.Repos) != 3 || o.Repos[0].Repo != "org/a" || o.Repos[1].Repo != "org/b" || o.Repos[2].Repo != "org/c" {
		t.Errorf("repo order = %+v, want org/a, org/b, org/c", o.Repos)
	}
	if o.Repos[0].TopTool != "Claude" || o.Repos[1].TopTool != "Claude" {
		t.Errorf("top tools = %q / %q, want Claude / Claude (ties broken by name)", o.Repos[0].TopTool, o.Repos[1].TopTool)
	}
	if !strings.Contains(o.Summary, "3 repositories") || !strings.Contains(o.Summary, "2 of 3") {
		t.Errorf("summary = %q", o.Summary)
	}
}

func TestMerge_isOrderIndependent(t *testing.T) {
	a := repoReport("org/a", []Commit{{Hash: "1", AITool: "Claude", Insertions: 10, Date: mustTime("2026-07-06")}})
	b := repoReport("org/b", []Commit{{Hash: "2", Insertions: 10, Date: mustTime("2026-07-06")}})
	x, y := Merge([]Report{a, b}), Merge([]Report{b, a})
	if x.Summary != y.Summary || len(x.Repos) != len(y.Repos) || x.Repos[0].Repo != y.Repos[0].Repo {
		t.Errorf("merge depends on input order:\n%+v\n%+v", x, y)
	}
}

func TestMerge_bucketsSumByPeriod(t *testing.T) {
	a := repoReport("org/a", []Commit{
		{Hash: "1", AITool: "Claude", Date: mustTime("2026-07-06")}, // W28
		{Hash: "2", Date: mustTime("2026-07-14")},                   // W29
	})
	b := repoReport("org/b", []Commit{
		{Hash: "3", Date: mustTime("2026-07-08")}, // W28
	})
	o := Merge([]Report{a, b})
	if o.BucketGranularity != "week" {
		t.Fatalf("granularity = %q, want week", o.BucketGranularity)
	}
	if len(o.Buckets) != 2 {
		t.Fatalf("buckets = %+v, want W28 and W29", o.Buckets)
	}
	w28 := o.Buckets[0]
	if w28.Label != "2026-W28" || w28.TotalCommits != 2 || w28.AICommits != 1 || w28.AIShare != 0.5 {
		t.Errorf("W28 = %+v, want 2 commits, 1 AI, share 0.5", w28)
	}
}

func TestMerge_rollsWeeklyBucketsUpWhenAnyRepoIsMonthly(t *testing.T) {
	short := repoReport("org/short", []Commit{
		{Hash: "1", AITool: "Claude", Date: mustTime("2026-07-06")}, // weekly bucket 2026-W28
	})
	long := repoReport("org/long", []Commit{ // > 26 weeks span → monthly buckets
		{Hash: "2", Date: mustTime("2026-01-15")},
		{Hash: "3", AITool: "Cursor", Date: mustTime("2026-07-20")},
	})
	if short.BucketGranularity != "week" || long.BucketGranularity != "month" {
		t.Fatalf("fixture granularity = %q / %q", short.BucketGranularity, long.BucketGranularity)
	}
	o := Merge([]Report{short, long})
	if o.BucketGranularity != "month" {
		t.Fatalf("merged granularity = %q, want month", o.BucketGranularity)
	}
	var july *TimeBucket
	for i := range o.Buckets {
		if strings.Contains(o.Buckets[i].Label, "-W") {
			t.Errorf("weekly label survived the roll-up: %+v", o.Buckets[i])
		}
		if o.Buckets[i].Label == "2026-07" {
			july = &o.Buckets[i]
		}
	}
	if july == nil || july.TotalCommits != 2 || july.AICommits != 2 {
		t.Errorf("July = %+v, want both repos' July commits (2, both AI)", july)
	}
}

func TestGranularityOf_infersFromLabelsForOlderReports(t *testing.T) {
	weekly := Report{Buckets: []TimeBucket{{Label: "2026-W28", Start: "2026-07-06"}}}
	monthly := Report{Buckets: []TimeBucket{{Label: "2026-07", Start: "2026-07-01"}}}
	if g := granularityOf(weekly); g != "week" {
		t.Errorf("weekly labels → %q, want week", g)
	}
	if g := granularityOf(monthly); g != "month" {
		t.Errorf("monthly labels → %q, want month", g)
	}
	if g := granularityOf(Report{}); g != "" {
		t.Errorf("no buckets → %q, want empty", g)
	}
}

func TestMerge_empty(t *testing.T) {
	o := Merge(nil)
	if o.ReposCovered != 0 || o.TotalCommits != 0 || o.Summary != "No repositories" {
		t.Errorf("empty merge = %+v", o)
	}
	if md := RenderOrgMarkdown(o); !strings.Contains(md, "No repositories") {
		t.Errorf("markdown for empty merge:\n%s", md)
	}
	if doc := RenderOrgHTML(o, time.Now()); !strings.Contains(doc, "No repositories") {
		t.Errorf("html for empty merge should say so")
	}
}

func TestRenderOrgMarkdown(t *testing.T) {
	a := repoReport("org/a|pipe", []Commit{
		{Hash: "1", AITool: "Claude", Insertions: 1200, Date: mustTime("2026-07-06")},
		{Hash: "2", Insertions: 100, Date: mustTime("2026-07-07")},
	})
	md := RenderOrgMarkdown(Merge([]Report{a}))
	for _, want := range []string{
		"## 📊 AI Attribution — organization — since 90 days ago",
		"| Repositories | 1 scanned · 1 with AI-assisted commits |",
		"| Commits | 2 total · 1 AI-assisted (50%) · 1 human |",
		"| Changed lines | 1,300 total · 1,200 AI-assisted (92%) |",
		"**By tool:** Claude (1)",
		"| org/a\\|pipe | 2 | 1 | 50% | 92% | Claude |",
		"not forensic detection",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestRenderOrgHTML(t *testing.T) {
	a := repoReport("org/<a>", []Commit{
		{Hash: "1", AITool: "Claude", Insertions: 10, Date: mustTime("2026-07-06")},
		{Hash: "2", Insertions: 10, Date: mustTime("2026-07-07")},
	})
	b := repoReport("org/b", []Commit{{Hash: "3", Insertions: 10, Date: mustTime("2026-07-08")}})
	doc := RenderOrgHTML(Merge([]Report{a, b}), time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"<title>ODS AI Attribution — Organization</title>",
		"2 repositories",
		`<div class="label">AI commit share</div><div class="value">33%</div>`,
		"org/&lt;a&gt;",         // escaped
		`<table class="repos">`, // per-repo table
		"<svg",                  // merged trend
		"Claude",
		"2026-09-15 00:00 UTC",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("html missing %q", want)
		}
	}
	if strings.Contains(doc, "org/<a>") {
		t.Errorf("repository name was not escaped")
	}
}

// mixedCommits returns n undated commits, the first ai of them attributed to
// Claude.
func mixedCommits(n, ai int) []Commit {
	out := make([]Commit, n)
	for i := range out {
		out[i] = Commit{Hash: fmt.Sprintf("c%d", i)}
		if i < ai {
			out[i].AITool = "Claude"
		}
	}
	return out
}

// permutations returns every ordering of reports.
func permutations(reports []Report) [][]Report {
	if len(reports) <= 1 {
		return [][]Report{slices.Clone(reports)}
	}
	var out [][]Report
	for i := range reports {
		for _, rest := range permutations(slices.Concat(reports[:i], reports[i+1:])) {
			out = append(out, append([]Report{reports[i]}, rest...))
		}
	}
	return out
}

// TestMerge_ordersTiesByVolumeThenName guards the repository table order: most
// AI-heavy first, equal shares broken by commit volume (busiest first), equal
// volume broken by name. The expected order must come out of every possible
// input order, since the table has to be stable from run to run.
func TestMerge_ordersTiesByVolumeThenName(t *testing.T) {
	reports := []Report{
		repoReport("org/none", mixedCommits(3, 0)),  // no AI at all: last
		repoReport("org/zeta", mixedCommits(1, 1)),  // 100%: first, however small and late in the alphabet
		repoReport("org/big", mixedCommits(4, 2)),   // 50% over 4 commits: busiest of the ties
		repoReport("org/beta", mixedCommits(2, 1)),  // 50% over 2 commits, ties with alpha
		repoReport("org/alpha", mixedCommits(2, 1)), // ... and wins that tie on name
	}
	want := []string{"org/zeta", "org/big", "org/alpha", "org/beta", "org/none"}

	for _, in := range permutations(reports) {
		var got, order []string
		for _, row := range Merge(in).Repos {
			got = append(got, row.Repo)
		}
		for _, r := range in {
			order = append(order, r.Repo)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("input order %v gave repos %v, want %v", order, got, want)
		}
	}
}

// TestMerge_joinsDistinctWindows guards the window label of a mixed batch:
// every distinct window appears once, in first-seen order, and reports that
// recorded no window add nothing to it.
func TestMerge_joinsDistinctWindows(t *testing.T) {
	o := Merge([]Report{
		{Repo: "org/a", Since: "90 days ago"},
		{Repo: "org/b", Since: "30 days ago"},
		{Repo: "org/c", Since: "90 days ago"},
		{Repo: "org/d"},
	})
	if want := "90 days ago / 30 days ago"; o.Since != want {
		t.Errorf("since = %q, want %q", o.Since, want)
	}
	if o.ReposCovered != 4 {
		t.Errorf("repos covered = %d, want 4 (a missing window does not drop the repository)", o.ReposCovered)
	}
}

// TestMerge_rollUpSkipsWeeklyBucketsWithoutAUsableStart guards the weekly to
// monthly roll-up against reports whose weekly buckets carry no usable start
// date (hand-edited or foreign JSON): a week that cannot be placed in a month
// is left out of the trend instead of crashing the merge or landing in a
// bogus month, while the well-formed buckets still sum.
func TestMerge_rollUpSkipsWeeklyBucketsWithoutAUsableStart(t *testing.T) {
	monthly := Report{Repo: "org/long", BucketGranularity: "month", Buckets: []TimeBucket{
		{Label: "2026-07", Start: "2026-07-01", TotalCommits: 4, AICommits: 1, AIShare: 0.25},
	}}
	weekly := Report{Repo: "org/short", BucketGranularity: "week", Buckets: []TimeBucket{
		{Label: "2026-W28", Start: "2026-07-06", TotalCommits: 2, AICommits: 2, AIShare: 1},
		{Label: "2026-W29", Start: "", TotalCommits: 5, AICommits: 5, AIShare: 1},       // start missing
		{Label: "2026-W30", Start: "2026-0", TotalCommits: 7, AICommits: 7, AIShare: 1}, // start cut short
	}}

	o := Merge([]Report{monthly, weekly})
	want := []TimeBucket{{Label: "2026-07", Start: "2026-07-01", TotalCommits: 6, AICommits: 3, AIShare: 0.5}}
	if o.BucketGranularity != "month" || !slices.Equal(o.Buckets, want) {
		t.Errorf("trend = %q %+v, want month %+v", o.BucketGranularity, o.Buckets, want)
	}
}

// TestMerge_rollUpFilesAWeekUnderTheMonthItStarts guards the documented
// roll-up rule: a weekly bucket goes to the month of its start date, so the
// ISO week that begins in December (2026-W01 starts on 2025-12-29) lands in
// December even though its label says 2026.
func TestMerge_rollUpFilesAWeekUnderTheMonthItStarts(t *testing.T) {
	weekly := Report{Repo: "org/a", BucketGranularity: "week", Buckets: []TimeBucket{
		{Label: "2026-W01", Start: "2025-12-29", TotalCommits: 3, AICommits: 1},
	}}
	monthly := Report{Repo: "org/b", BucketGranularity: "month", Buckets: []TimeBucket{
		{Label: "2025-12", Start: "2025-12-01", TotalCommits: 1, AICommits: 1},
		{Label: "2026-02", Start: "2026-02-01", TotalCommits: 2},
	}}

	o := Merge([]Report{weekly, monthly})
	want := []TimeBucket{
		{Label: "2025-12", Start: "2025-12-01", TotalCommits: 4, AICommits: 2, AIShare: 0.5},
		{Label: "2026-02", Start: "2026-02-01", TotalCommits: 2},
	}
	if !slices.Equal(o.Buckets, want) {
		t.Errorf("buckets = %+v, want %+v", o.Buckets, want)
	}
}

// TestMerge_monthlyGranularityWinsInAnyOrder guards that one monthly report
// turns the whole trend monthly wherever it sits in the batch: a weekly report
// after it must not pull the granularity back down.
func TestMerge_monthlyGranularityWinsInAnyOrder(t *testing.T) {
	short := repoReport("org/short", []Commit{{Hash: "1", AITool: "Claude", Date: mustTime("2026-07-06")}})
	long := repoReport("org/long", []Commit{
		{Hash: "2", Date: mustTime("2026-01-15")},
		{Hash: "3", AITool: "Cursor", Date: mustTime("2026-07-20")},
	})

	a, b := Merge([]Report{short, long}), Merge([]Report{long, short})
	if a.BucketGranularity != "month" || b.BucketGranularity != "month" {
		t.Errorf("granularity = %q / %q, want month for both orders", a.BucketGranularity, b.BucketGranularity)
	}
	if !slices.Equal(a.Buckets, b.Buckets) {
		t.Errorf("trend depends on report order:\n%+v\n%+v", a.Buckets, b.Buckets)
	}
}

// TestMerge_reportsWithoutCommits guards the "scanned but nothing to show"
// case: the repositories are still counted, the summary says there were no
// commits instead of printing 0% figures, and both renderers fall back to that
// one sentence with no tables, charts or attribution caveat.
func TestMerge_reportsWithoutCommits(t *testing.T) {
	o := Merge([]Report{repoReport("org/a", nil), repoReport("org/b", nil)})

	const summary = "2 repositories, no commits in the selected window"
	if o.ReposCovered != 2 || o.Summary != summary {
		t.Errorf("repos covered = %d, summary = %q, want 2 and %q", o.ReposCovered, o.Summary, summary)
	}
	if len(o.Buckets) != 0 || o.BucketGranularity != "" {
		t.Errorf("trend = %q %+v, want none", o.BucketGranularity, o.Buckets)
	}

	md := RenderOrgMarkdown(o)
	if !strings.Contains(md, summary+".") || strings.Contains(md, "| Metric |") {
		t.Errorf("markdown should be the one-line summary:\n%s", md)
	}
	doc := RenderOrgHTML(o, mustTime("2026-07-16"))
	if !strings.Contains(doc, `<p class="sub">`+summary+`.</p>`) {
		t.Errorf("html missing the summary sentence")
	}
	for _, unwanted := range []string{"<table", "<svg", "Attribution reflects what AI tools disclose"} {
		if strings.Contains(doc, unwanted) {
			t.Errorf("html for an empty window should not contain %q", unwanted)
		}
	}
}

// TestRenderOrg_humanOnlyHistoryHasNoToolBreakdown guards the organization
// views for repositories with commits but no AI work: the table row has an
// empty top tool and neither renderer invents a per-tool section.
func TestRenderOrg_humanOnlyHistoryHasNoToolBreakdown(t *testing.T) {
	o := Merge([]Report{repoReport("org/a", []Commit{{Hash: "1", Insertions: 5, Date: mustTime("2026-07-06")}})})

	md := RenderOrgMarkdown(o)
	if strings.Contains(md, "By tool") {
		t.Errorf("markdown has a tool line for human-only work:\n%s", md)
	}
	for _, want := range []string{"| Repositories | 1 scanned · 0 with AI-assisted commits |", "| org/a | 1 | 0 | 0% | 0% |  |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}

	doc := RenderOrgHTML(o, mustTime("2026-07-16"))
	if strings.Contains(doc, "By tool") {
		t.Error("html has a tool section for human-only work")
	}
	if !strings.Contains(doc, `<table class="repos">`) {
		t.Error("html lost the repository table")
	}
}

// TestMdCell guards the Markdown table cells: pipes and line breaks in a
// repository or tool name cannot split a row.
func TestMdCell(t *testing.T) {
	cases := map[string]string{
		"org/a":      "org/a",
		"org/a|b":    `org/a\|b`,
		"two\nlines": "two lines",
		"a|b\nc|d":   `a\|b c\|d`,
		"":           "",
	}
	for in, want := range cases {
		if got := mdCell(in); got != want {
			t.Errorf("mdCell(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRenderOrgHTML_escapesSinceAndTopTool guards the two fields the
// organization dashboard takes from report JSON without going through a
// repository name: the window text and each repository's top tool.
func TestRenderOrgHTML_escapesSinceAndTopTool(t *testing.T) {
	o := Merge([]Report{repoReport("org/a", []Commit{
		{Hash: "1", AITool: "<i>tool</i>", Insertions: 1, Date: mustTime("2026-07-06")},
	})})
	o.Since = "<script>1</script>"

	doc := RenderOrgHTML(o, mustTime("2026-07-16"))
	for _, raw := range []string{"<script>1</script>", "<i>tool</i>"} {
		if strings.Contains(doc, raw) {
			t.Errorf("html contains unescaped %q", raw)
		}
	}
	for _, want := range []string{"&lt;script&gt;1&lt;/script&gt;", "&lt;i&gt;tool&lt;/i&gt;"} {
		if !strings.Contains(doc, want) {
			t.Errorf("html missing escaped %q", want)
		}
	}
}

// TestMerge_acceptsReportsRoundTrippedThroughJSON guards the premise of the
// organization view: it is computed from the repositories' JSON, never from
// git, so a report that has been written out and read back must merge to
// exactly what the in-memory report does.
func TestMerge_acceptsReportsRoundTrippedThroughJSON(t *testing.T) {
	orig := repoReport("org/a", []Commit{
		{Hash: "1", AITool: "Claude", Insertions: 30, Deletions: 4, Date: mustTime("2026-07-06")},
		{Hash: "2", AITool: "Cursor", Insertions: 5, Date: mustTime("2026-07-14")},
		{Hash: "3", Insertions: 12, Date: mustTime("2026-07-15")},
	})
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Report
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if direct, viaJSON := Merge([]Report{orig}), Merge([]Report{back}); !reflect.DeepEqual(direct, viaJSON) {
		t.Errorf("merge changed across a JSON round trip:\n%+v\n%+v", direct, viaJSON)
	}
}
