package report

import (
	"math"
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
