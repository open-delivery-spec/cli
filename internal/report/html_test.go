package report

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Patterns that read the pieces of a rendered trend chart back out of its SVG,
// so the tests assert on bar heights and labels rather than on exact markup.
var (
	svgTextRE  = regexp.MustCompile(`<text [^>]*>([^<]*)</text>`)
	svgWidthRE = regexp.MustCompile(`<svg [^>]*width="(\d+)"`)
	barWidthRE = regexp.MustCompile(`style="width:(\d+)%"`)
)

// svgRect is one bar rectangle of a rendered chart. The grey track behind a
// bucket carries a label (from its tooltip); the AI segment drawn over it does
// not.
type svgRect struct {
	x, y, h int
	label   string
}

var svgRectRE = regexp.MustCompile(`<rect x="(\d+)" y="(\d+)" width="\d+" height="(\d+)" rx="2" fill="#[0-9a-f]{6}"(?:><title>([^:<]*):)?`)

// chartRects returns every bar rectangle of a rendered chart in drawing order.
func chartRects(t *testing.T, svg string) []svgRect {
	t.Helper()
	num := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("rect coordinate %q: %v", s, err)
		}
		return n
	}
	var out []svgRect
	for _, m := range svgRectRE.FindAllStringSubmatch(svg, -1) {
		out = append(out, svgRect{x: num(m[1]), y: num(m[2]), h: num(m[3]), label: m[4]})
	}
	return out
}

// barHeights maps each bucket label of a rendered chart to its bar's height in
// pixels.
func barHeights(t *testing.T, svg string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, r := range chartRects(t, svg) {
		if r.label != "" {
			out[r.label] = r.h
		}
	}
	return out
}

// chartLabels splits the text of a rendered chart into the x-axis labels and
// the share percentages drawn above the bars, which are the only text ending
// in "%".
func chartLabels(svg string) (axis, shares []string) {
	for _, m := range svgTextRE.FindAllStringSubmatch(svg, -1) {
		if strings.HasSuffix(m[1], "%") {
			shares = append(shares, m[1])
		} else {
			axis = append(axis, m[1])
		}
	}
	return axis, shares
}

// weekBuckets returns n one-commit buckets labeled 2026-W01 onward.
func weekBuckets(n int) []TimeBucket {
	out := make([]TimeBucket, n)
	for i := range out {
		out[i] = TimeBucket{Label: fmt.Sprintf("2026-W%02d", i+1), TotalCommits: 1}
	}
	return out
}

// axisRange returns the axis labels W<from>, W<from+step>, ... up to W<to>.
func axisRange(from, to, step int) []string {
	var out []string
	for i := from; i <= to; i += step {
		out = append(out, fmt.Sprintf("W%02d", i))
	}
	return out
}

// TestTrendSection guards the trend block: no buckets means no section at all
// (no heading over an empty chart), otherwise heading, legend and chart.
func TestTrendSection(t *testing.T) {
	for name, buckets := range map[string][]TimeBucket{"nil": nil, "empty": {}} {
		if got := trendSection(buckets); got != "" {
			t.Errorf("trendSection(%s) = %q, want nothing", name, got)
		}
	}

	got := trendSection(weekBuckets(2))
	for _, want := range []string{"AI share over time", "AI-assisted", "human", "<svg", "</svg>"} {
		if !strings.Contains(got, want) {
			t.Errorf("trend section missing %q", want)
		}
	}
}

// TestToolSection guards the per-tool bars: nothing without tools, the busiest
// tool at full width with the rest scaled to it, and no NaN or Inf width when
// every count is zero (a report loaded from JSON can say so).
func TestToolSection(t *testing.T) {
	t.Run("no tools renders nothing", func(t *testing.T) {
		if got := toolSection(nil); got != "" {
			t.Errorf("toolSection(nil) = %q, want nothing", got)
		}
	})

	t.Run("bars scale to the busiest tool", func(t *testing.T) {
		got := toolSection([]ToolCount{{Tool: "Claude", Commits: 4}, {Tool: "Cursor", Commits: 2}, {Tool: "Codex", Commits: 1}})
		var widths []string
		for _, m := range barWidthRE.FindAllStringSubmatch(got, -1) {
			widths = append(widths, m[1])
		}
		if want := []string{"100", "50", "25"}; !slices.Equal(widths, want) {
			t.Errorf("bar widths = %v, want %v", widths, want)
		}
		for _, want := range []string{"By tool", ">Claude<", ">Cursor<", ">Codex<", `<span class="tool-count">4</span>`} {
			if !strings.Contains(got, want) {
				t.Errorf("tool section missing %q", want)
			}
		}
	})

	t.Run("zero counts draw empty bars", func(t *testing.T) {
		got := toolSection([]ToolCount{{Tool: "Claude"}, {Tool: "Cursor"}})
		if strings.Contains(got, "NaN") || strings.Contains(got, "Inf") {
			t.Errorf("zero counts produced a non-finite width:\n%s", got)
		}
		if n := strings.Count(got, "width:0%"); n != 2 {
			t.Errorf("zero-width bars = %d, want 2:\n%s", n, got)
		}
		if !strings.Contains(got, `<span class="tool-count">0</span>`) {
			t.Errorf("zero count not shown:\n%s", got)
		}
	})
}

// TestRenderTrendSVG_keepsTinyBucketsVisible guards the minimum bar height: a
// bucket holding a sliver of the busiest bucket's volume would round down to
// zero pixels and vanish, so it is clamped. A bucket with no commits at all
// stays flat and gets neither an AI segment nor a share label.
func TestRenderTrendSVG_keepsTinyBucketsVisible(t *testing.T) {
	svg := renderTrendSVG([]TimeBucket{
		{Label: "2026-W01", TotalCommits: 1000, AICommits: 500, AIShare: 0.5},
		{Label: "2026-W02", TotalCommits: 1, AICommits: 1, AIShare: 1},
		{Label: "2026-W03"},
	})

	h := barHeights(t, svg)
	if h["2026-W01"] <= h["2026-W02"] {
		t.Errorf("busiest bar (%dpx) should be taller than the tiny one (%dpx)", h["2026-W01"], h["2026-W02"])
	}
	if h["2026-W02"] < 2 {
		t.Errorf("tiny bucket drawn %dpx tall, want at least 2px so it stays visible", h["2026-W02"])
	}
	if h["2026-W03"] != 0 {
		t.Errorf("empty bucket drawn %dpx tall, want 0", h["2026-W03"])
	}
	if n := strings.Count(svg, `fill="#a371f7"`); n != 2 {
		t.Errorf("AI segments = %d, want 2 (the empty bucket has none)", n)
	}
	if _, shares := chartLabels(svg); !slices.Equal(shares, []string{"50%", "100%"}) {
		t.Errorf("share labels = %v, want [50%% 100%%] (none for the empty bucket)", shares)
	}
}

// TestRenderTrendSVG_thinsAxisLabels guards the x-axis: every bucket is
// labeled up to ten of them, longer series are thinned to every Nth bucket
// starting with the first, so the labels never run into each other.
func TestRenderTrendSVG_thinsAxisLabels(t *testing.T) {
	cases := []struct {
		buckets int
		want    []string
	}{
		{1, axisRange(1, 1, 1)},
		{10, axisRange(1, 10, 1)}, // the last count that labels every bucket
		{11, axisRange(1, 11, 2)}, // first thinned series: every 2nd
		{17, axisRange(1, 17, 3)},
		{25, axisRange(1, 25, 4)},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d buckets", tc.buckets), func(t *testing.T) {
			axis, _ := chartLabels(renderTrendSVG(weekBuckets(tc.buckets)))
			if !slices.Equal(axis, tc.want) {
				t.Errorf("axis labels = %v, want %v", axis, tc.want)
			}
		})
	}
}

// TestRenderTrendSVG_neverCrowdsTheAxis checks the thinning holds for any
// series length: at most eight labels, and the first bucket is always one.
func TestRenderTrendSVG_neverCrowdsTheAxis(t *testing.T) {
	for n := 11; n <= 120; n++ {
		axis, _ := chartLabels(renderTrendSVG(weekBuckets(n)))
		if len(axis) > 8 {
			t.Fatalf("%d buckets drew %d axis labels, want at most 8", n, len(axis))
		}
		if len(axis) == 0 || axis[0] != "W01" {
			t.Fatalf("%d buckets: axis starts %v, want W01 first", n, axis)
		}
	}
}

// TestRenderTrendSVG_widthFollowsBucketCount guards the chart width: every
// series gets at least the readable minimum, and a long one grows instead of
// squeezing its bars (the page scrolls the chart sideways).
func TestRenderTrendSVG_widthFollowsBucketCount(t *testing.T) {
	width := func(n int) int {
		m := svgWidthRE.FindStringSubmatch(renderTrendSVG(weekBuckets(n)))
		if m == nil {
			t.Fatalf("no width in the SVG for %d buckets", n)
		}
		w, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("width %q: %v", m[1], err)
		}
		return w
	}

	if w := width(1); w != 320 {
		t.Errorf("one bucket: width = %d, want the 320 minimum", w)
	}
	prev := 0
	for n := 1; n <= 30; n++ {
		w := width(n)
		if w < 320 {
			t.Errorf("%d buckets: width = %d, want at least the 320 minimum", n, w)
		}
		if w < prev {
			t.Errorf("%d buckets: width = %d, narrower than the %d before it", n, w, prev)
		}
		prev = w
	}
	if width(30) <= width(12) || width(12) <= 320 {
		t.Errorf("widths for 12 and 30 buckets = %d, %d, want growing past the minimum", width(12), width(30))
	}
}

// TestRenderTrendSVG_stacksAISegmentOnTheBaseline guards what the chart
// encodes: each bar's AI segment is the bucket's AI share of the bar height and
// is stacked on the same baseline as its bar, and a bucket without AI commits
// draws no segment.
func TestRenderTrendSVG_stacksAISegmentOnTheBaseline(t *testing.T) {
	buckets := []TimeBucket{
		{Label: "2026-W01", TotalCommits: 4, AICommits: 1, AIShare: 0.25},
		{Label: "2026-W02", TotalCommits: 4, AICommits: 2, AIShare: 0.5},
		{Label: "2026-W03", TotalCommits: 4, AICommits: 4, AIShare: 1},
		{Label: "2026-W04", TotalCommits: 4},
		{Label: "2026-W05", TotalCommits: 2, AICommits: 1, AIShare: 0.5}, // a shorter bar: the segment is
		{Label: "2026-W06", TotalCommits: 1, AICommits: 1, AIShare: 1},   // a share of it, not of the plot
	}
	tracks := map[string]svgRect{} // by label
	segments := map[int]svgRect{}  // by x, the bar they sit on
	for _, r := range chartRects(t, renderTrendSVG(buckets)) {
		if r.label != "" {
			tracks[r.label] = r
		} else {
			segments[r.x] = r
		}
	}

	if len(tracks) != len(buckets) || len(segments) != 5 {
		t.Fatalf("drew %d bars and %d AI segments, want 6 and 5", len(tracks), len(segments))
	}
	for _, bk := range buckets {
		bar := tracks[bk.Label]
		seg, ok := segments[bar.x]
		if bk.AICommits == 0 {
			if ok {
				t.Errorf("%s: human-only bucket drew an AI segment %+v", bk.Label, seg)
			}
			continue
		}
		if !ok {
			t.Errorf("%s: no AI segment", bk.Label)
			continue
		}
		if want := bar.h * bk.AICommits / bk.TotalCommits; seg.h != want {
			t.Errorf("%s: AI segment is %dpx of a %dpx bar, want %dpx (%d of %d commits)",
				bk.Label, seg.h, bar.h, want, bk.AICommits, bk.TotalCommits)
		}
		if seg.y+seg.h != bar.y+bar.h {
			t.Errorf("%s: AI segment ends at %d, bar ends at %d, want the same baseline", bk.Label, seg.y+seg.h, bar.y+bar.h)
		}
	}
}

// TestRenderTrendSVG_describesAndEscapes guards the per-bar tooltip and the
// accessible name of the chart, and that bucket labels (which come from report
// JSON) cannot inject markup.
func TestRenderTrendSVG_describesAndEscapes(t *testing.T) {
	svg := renderTrendSVG([]TimeBucket{
		{Label: "2026-W28", TotalCommits: 3, AICommits: 2, AIShare: 2.0 / 3},
		{Label: "<b>&", TotalCommits: 1},
	})
	for _, want := range []string{
		`aria-label="AI commit share over time"`,
		"<title>2026-W28: 3 commits, 67% AI</title>",
		"&lt;b&gt;&amp;",
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("SVG missing %q", want)
		}
	}
	if strings.Contains(svg, "<b>") {
		t.Errorf("bucket label was not escaped:\n%s", svg)
	}
}

// TestShortLabel guards the x-axis label shortening for both bucket label
// shapes, and that anything else passes through untouched.
func TestShortLabel(t *testing.T) {
	cases := map[string]string{
		"2026-W28": "W28", // weekly
		"2026-07":  "07",  // monthly
		"latest":   "latest",
		"W28":      "W28", // no dash to cut at
		"":         "",
	}
	for in, want := range cases {
		if got := shortLabel(in); got != want {
			t.Errorf("shortLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestHumanizeInt guards the thousands separators around their boundaries.
func TestHumanizeInt(t *testing.T) {
	cases := map[int]string{
		0:          "0",
		7:          "7",
		999:        "999",
		1000:       "1,000",
		12345:      "12,345",
		100000:     "100,000",
		1234567:    "1,234,567",
		1000000000: "1,000,000,000",
	}
	for in, want := range cases {
		if got := humanizeInt(in); got != want {
			t.Errorf("humanizeInt(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestRenderHTML_omitsTrendWithoutDates guards the dashboard when no commit
// carries a date (a report built from fixtures): the cards and tool bars still
// render, but there is no trend section or empty chart.
func TestRenderHTML_omitsTrendWithoutDates(t *testing.T) {
	r := Aggregate([]Commit{{Hash: "a", AITool: "Claude", Insertions: 5}}, "30 days ago")

	doc := RenderHTML(r, mustTime("2026-07-16"))
	for _, want := range []string{"AI commit share", "By tool", ">Claude<"} {
		if !strings.Contains(doc, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	for _, unwanted := range []string{"AI share over time", "<svg"} {
		if strings.Contains(doc, unwanted) {
			t.Errorf("HTML for undated commits should not contain %q", unwanted)
		}
	}
}

// TestRenderHTML_omitsToolSectionForHumanOnlyHistory guards the dashboard for
// a window without AI work: the trend still shows (at 0%), but there is no
// per-tool section to draw.
func TestRenderHTML_omitsToolSectionForHumanOnlyHistory(t *testing.T) {
	r := Aggregate([]Commit{{Hash: "a", Insertions: 5, Date: mustTime("2026-07-06")}}, "30 days ago")

	doc := RenderHTML(r, mustTime("2026-07-16"))
	if strings.Contains(doc, "By tool") {
		t.Error("human-only history should have no tool section")
	}
	for _, want := range []string{`<div class="value">0%</div>`, "AI share over time", "<svg"} {
		if !strings.Contains(doc, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

// TestRenderHTML_footerNoteOnlyWithData guards the attribution caveat: it
// qualifies numbers, so the empty-state page, which has none, leaves it off.
func TestRenderHTML_footerNoteOnlyWithData(t *testing.T) {
	const note = "Attribution reflects what AI tools disclose"
	full := RenderHTML(Aggregate([]Commit{{Hash: "a", Insertions: 1, Date: mustTime("2026-07-06")}}, "x"), mustTime("2026-07-16"))
	if !strings.Contains(full, note) {
		t.Error("report with data should carry the attribution note")
	}
	empty := RenderHTML(Aggregate(nil, "x"), mustTime("2026-07-16"))
	if strings.Contains(empty, note) {
		t.Error("empty-state page should not carry the attribution note")
	}
}

// TestRenderHTML_escapesSince guards the window text in the header: it comes
// straight from the --since flag or from report JSON.
func TestRenderHTML_escapesSince(t *testing.T) {
	r := Aggregate([]Commit{{Hash: "a", Insertions: 1, Date: mustTime("2026-07-06")}}, "<img src=x onerror=1>")

	doc := RenderHTML(r, mustTime("2026-07-16"))
	if strings.Contains(doc, "<img") {
		t.Error("since window must be HTML-escaped")
	}
	if !strings.Contains(doc, "&lt;img src=x onerror=1&gt;") {
		t.Error("escaped since window missing from the header")
	}
}

// TestDashboards_renderGeneratedTimeInUTC guards the footer timestamp of both
// dashboards: whatever zone the caller's clock is in, the page says UTC and
// shows the UTC time.
func TestDashboards_renderGeneratedTimeInUTC(t *testing.T) {
	// 23:30 at UTC+5 is 18:30 UTC the same day.
	at := time.Date(2026, time.July, 16, 23, 30, 0, 0, time.FixedZone("UTC+5", 5*3600))
	commits := []Commit{{Hash: "a", Insertions: 1, Date: mustTime("2026-07-06")}}

	repo := RenderHTML(Aggregate(commits, "x"), at)
	org := RenderOrgHTML(Merge([]Report{repoReport("org/a", commits)}), at)
	for name, doc := range map[string]string{"repository": repo, "organization": org} {
		if !strings.Contains(doc, "2026-07-16 18:30 UTC") {
			t.Errorf("%s dashboard footer is not in UTC", name)
		}
	}
}
