package report

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"
)

// OrgReport aggregates per-repository reports into one organization-wide
// view: the same numbers as Report summed across repositories, plus a
// per-repository breakdown. It is computed from the reports' JSON, never from
// git, so repositories can be scanned wherever they live and merged anywhere.
type OrgReport struct {
	Since             string         `json:"since"`
	Repos             []RepoSummary  `json:"repos"`
	ReposCovered      int            `json:"repos_covered"`
	ReposWithAI       int            `json:"repos_with_ai"`
	TotalCommits      int            `json:"total_commits"`
	AICommits         int            `json:"ai_commits"`
	HumanCommits      int            `json:"human_commits"`
	AICommitShare     float64        `json:"ai_commit_share"` // 0..1
	TotalChangedLines int            `json:"total_changed_lines"`
	AIChangedLines    int            `json:"ai_changed_lines"`
	AILineShare       float64        `json:"ai_line_share"` // 0..1
	ByTool            map[string]int `json:"by_tool"`       // tool -> commit count
	Buckets           []TimeBucket   `json:"buckets,omitempty"`
	BucketGranularity string         `json:"bucket_granularity,omitempty"`
	Summary           string         `json:"summary"`
}

// RepoSummary is one repository's row in the organization view.
type RepoSummary struct {
	Repo              string  `json:"repo"`
	TotalCommits      int     `json:"total_commits"`
	AICommits         int     `json:"ai_commits"`
	AICommitShare     float64 `json:"ai_commit_share"`
	TotalChangedLines int     `json:"total_changed_lines"`
	AIChangedLines    int     `json:"ai_changed_lines"`
	AILineShare       float64 `json:"ai_line_share"`
	TopTool           string  `json:"top_tool,omitempty"`
}

// Merge combines per-repository reports into an organization report. Pure and
// deterministic: the same inputs always produce the same output, whatever
// their order.
func Merge(reports []Report) OrgReport {
	o := OrgReport{ByTool: map[string]int{}}

	var sinces []string
	seenSince := map[string]bool{}
	for _, r := range reports {
		if r.Since != "" && !seenSince[r.Since] {
			seenSince[r.Since] = true
			sinces = append(sinces, r.Since)
		}
		row := RepoSummary{
			Repo:              r.Repo,
			TotalCommits:      r.TotalCommits,
			AICommits:         r.AICommits,
			AICommitShare:     r.AICommitShare,
			TotalChangedLines: r.TotalChangedLines,
			AIChangedLines:    r.AIChangedLines,
			AILineShare:       r.AILineShare,
		}
		if tools := r.ToolBreakdown(); len(tools) > 0 {
			row.TopTool = tools[0].Tool
		}
		o.Repos = append(o.Repos, row)

		o.ReposCovered++
		if r.AICommits > 0 {
			o.ReposWithAI++
		}
		o.TotalCommits += r.TotalCommits
		o.AICommits += r.AICommits
		o.HumanCommits += r.HumanCommits
		o.TotalChangedLines += r.TotalChangedLines
		o.AIChangedLines += r.AIChangedLines
		for tool, n := range r.ByTool {
			o.ByTool[tool] += n
		}
	}
	o.Since = strings.Join(sinces, " / ")
	if o.TotalCommits > 0 {
		o.AICommitShare = float64(o.AICommits) / float64(o.TotalCommits)
	}
	if o.TotalChangedLines > 0 {
		o.AILineShare = float64(o.AIChangedLines) / float64(o.TotalChangedLines)
	}
	// Most AI-heavy repositories first; ties by volume, then name, so the
	// order is stable across runs.
	sort.Slice(o.Repos, func(i, j int) bool {
		a, b := o.Repos[i], o.Repos[j]
		if a.AICommitShare != b.AICommitShare {
			return a.AICommitShare > b.AICommitShare
		}
		if a.TotalCommits != b.TotalCommits {
			return a.TotalCommits > b.TotalCommits
		}
		return a.Repo < b.Repo
	})
	o.Buckets, o.BucketGranularity = mergeBuckets(reports)
	o.Summary = summarizeOrg(o)
	return o
}

// mergeBuckets sums the repositories' trend buckets by period. Repositories
// bucket weekly or monthly depending on their own span, so when any of them is
// monthly every weekly bucket is rolled up to the month of its start date.
func mergeBuckets(reports []Report) ([]TimeBucket, string) {
	target := ""
	for _, r := range reports {
		switch granularityOf(r) {
		case "month":
			target = "month"
		case "week":
			if target == "" {
				target = "week"
			}
		}
	}
	if target == "" {
		return nil, ""
	}

	idx := map[string]int{}
	var out []TimeBucket
	for _, r := range reports {
		for _, bk := range r.Buckets {
			label, start := bk.Label, bk.Start
			if target == "month" && strings.Contains(bk.Label, "-W") {
				if len(bk.Start) >= 7 {
					label, start = bk.Start[:7], bk.Start[:7]+"-01"
				} else {
					continue
				}
			}
			i, ok := idx[label]
			if !ok {
				i = len(out)
				idx[label] = i
				out = append(out, TimeBucket{Label: label, Start: start})
			}
			out[i].TotalCommits += bk.TotalCommits
			out[i].AICommits += bk.AICommits
		}
	}
	for i := range out {
		if out[i].TotalCommits > 0 {
			out[i].AIShare = float64(out[i].AICommits) / float64(out[i].TotalCommits)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out, target
}

// granularityOf returns a report's bucket granularity, inferring it from the
// bucket labels for reports written before the field existed.
func granularityOf(r Report) string {
	if r.BucketGranularity != "" {
		return r.BucketGranularity
	}
	if len(r.Buckets) == 0 {
		return ""
	}
	if strings.Contains(r.Buckets[0].Label, "-W") {
		return "week"
	}
	return "month"
}

func summarizeOrg(o OrgReport) string {
	if o.ReposCovered == 0 {
		return "No repositories"
	}
	if o.TotalCommits == 0 {
		return fmt.Sprintf("%d repositories, no commits in the selected window", o.ReposCovered)
	}
	return fmt.Sprintf(
		"%d repositories: %d commit(s), %d AI-assisted (%.0f%%) — AI touched %.0f%% of changed lines; %d of %d repositories show AI-assisted work",
		o.ReposCovered, o.TotalCommits, o.AICommits, o.AICommitShare*100, o.AILineShare*100, o.ReposWithAI, o.ReposCovered,
	)
}

// ToolBreakdown returns the per-tool commit counts sorted by count desc, then name.
func (o OrgReport) ToolBreakdown() []ToolCount {
	return Report{ByTool: o.ByTool}.ToolBreakdown()
}

// RenderOrgMarkdown renders the organization report as Markdown: a metrics
// table, the per-tool line and a per-repository table. Meant for a job
// summary, a README or a PR comment.
func RenderOrgMarkdown(o OrgReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## \U0001f4ca AI Attribution — organization — since %s\n\n", o.Since)
	if o.TotalCommits == 0 {
		fmt.Fprintf(&b, "%s.\n", o.Summary)
		return b.String()
	}
	fmt.Fprintf(&b, "| Metric | Value |\n|--------|-------|\n")
	fmt.Fprintf(&b, "| Repositories | %d scanned · %d with AI-assisted commits |\n", o.ReposCovered, o.ReposWithAI)
	fmt.Fprintf(&b, "| Commits | %d total · %d AI-assisted (%.0f%%) · %d human |\n",
		o.TotalCommits, o.AICommits, o.AICommitShare*100, o.HumanCommits)
	fmt.Fprintf(&b, "| Changed lines | %s total · %s AI-assisted (%.0f%%) |\n",
		humanizeInt(o.TotalChangedLines), humanizeInt(o.AIChangedLines), o.AILineShare*100)

	if tools := o.ToolBreakdown(); len(tools) > 0 {
		parts := make([]string, 0, len(tools))
		for _, tc := range tools {
			parts = append(parts, fmt.Sprintf("%s (%d)", tc.Tool, tc.Commits))
		}
		fmt.Fprintf(&b, "\n**By tool:** %s\n", strings.Join(parts, ", "))
	}

	b.WriteString("\n| Repository | Commits | AI-assisted | AI share | AI line share | Top tool |\n")
	b.WriteString("|------------|--------:|------------:|---------:|--------------:|----------|\n")
	for _, r := range o.Repos {
		fmt.Fprintf(&b, "| %s | %d | %d | %.0f%% | %.0f%% | %s |\n",
			mdCell(r.Repo), r.TotalCommits, r.AICommits, r.AICommitShare*100, r.AILineShare*100, mdCell(r.TopTool))
	}
	b.WriteString("\n_Attribution from `Co-Authored-By` / `Assisted-by` trailers — what AI tools disclose, not forensic detection._\n")
	return b.String()
}

// mdCell keeps repository and tool names from breaking a Markdown table.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}

// RenderOrgHTML renders a self-contained organization dashboard: hero
// metrics, the merged trend, the per-tool breakdown and a per-repository
// table. Same look as the repository dashboard, no external assets.
func RenderOrgHTML(o OrgReport, generatedAt time.Time) string {
	var b strings.Builder
	generated := generatedAt.UTC().Format("2006-01-02 15:04 UTC")

	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ODS AI Attribution — Organization</title>
` + dashboardCSS + `
</head>
<body>
<header>
  <h1>ODS AI Attribution — Organization</h1>
  <div class="sub">Since ` + html.EscapeString(o.Since) + ` · ` + fmt.Sprintf("%d", o.ReposCovered) + ` repositories · attribution from Co-Authored-By / Assisted-by trailers</div>
</header>
`)

	if o.TotalCommits == 0 {
		b.WriteString(`<p class="sub">` + html.EscapeString(o.Summary) + `.</p>
<footer>Generated by <a href="https://github.com/open-delivery-spec">Open Delivery Spec</a> · ` + generated + `</footer>
</body></html>`)
		return b.String()
	}

	fmt.Fprintf(&b, `<div class="cards">
  <div class="card hero"><div class="label">AI commit share</div><div class="value">%.0f%%</div><div class="sub">%d of %d commits</div></div>
  <div class="card hero"><div class="label">AI line share</div><div class="value">%.0f%%</div><div class="sub">%s of %s changed lines</div></div>
  <div class="card"><div class="label">Repositories</div><div class="value">%d</div><div class="sub">%d with AI-assisted commits</div></div>
  <div class="card"><div class="label">Commits</div><div class="value">%s</div><div class="sub">%s human · %s AI-assisted</div></div>
</div>
`, o.AICommitShare*100, o.AICommits, o.TotalCommits,
		o.AILineShare*100, humanizeInt(o.AIChangedLines), humanizeInt(o.TotalChangedLines),
		o.ReposCovered, o.ReposWithAI,
		humanizeInt(o.TotalCommits), humanizeInt(o.HumanCommits), humanizeInt(o.AICommits))

	if len(o.Buckets) > 0 {
		b.WriteString(`<div class="section"><h2>AI share over time</h2>
<div class="legend"><span class="dot" style="background:var(--ai)"></span>AI-assisted<span class="dot" style="background:var(--human)"></span>human</div>
<div class="chart-wrap">`)
		b.WriteString(renderTrendSVG(o.Buckets))
		b.WriteString(`</div></div>
`)
	}

	if tools := o.ToolBreakdown(); len(tools) > 0 {
		maxN := tools[0].Commits
		b.WriteString(`<div class="section"><h2>By tool</h2>`)
		for _, tc := range tools {
			w := 0.0
			if maxN > 0 {
				w = float64(tc.Commits) / float64(maxN) * 100
			}
			fmt.Fprintf(&b, `<div class="tool-row"><span class="tool-name">%s</span><span class="tool-bar-track"><span class="tool-bar-fill" style="width:%.0f%%"></span></span><span class="tool-count">%d</span></div>
`, html.EscapeString(tc.Tool), w, tc.Commits)
		}
		b.WriteString(`</div>
`)
	}

	b.WriteString(`<div class="section"><h2>By repository</h2>
<table class="repos"><thead><tr><th>Repository</th><th class="num">Commits</th><th class="num">AI-assisted</th><th>AI share</th><th class="num">AI line share</th><th>Top tool</th></tr></thead>
<tbody>
`)
	for _, r := range o.Repos {
		fmt.Fprintf(&b, `<tr><td>%s</td><td class="num">%d</td><td class="num">%d</td><td><span class="share">%.0f%%</span><span class="bar"><span style="width:%.0f%%"></span></span></td><td class="num">%.0f%%</td><td>%s</td></tr>
`, html.EscapeString(r.Repo), r.TotalCommits, r.AICommits, r.AICommitShare*100, r.AICommitShare*100, r.AILineShare*100, html.EscapeString(r.TopTool))
	}
	b.WriteString(`</tbody></table></div>
`)

	b.WriteString(`<footer>Generated by <a href="https://github.com/open-delivery-spec">Open Delivery Spec</a> · ` + generated + ` · Attribution reflects what AI tools disclose, not forensic detection.</footer>
</body></html>`)
	return b.String()
}
