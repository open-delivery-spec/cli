package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/open-delivery-spec/cli/internal/validator"
	"github.com/open-delivery-spec/cli/internal/version"
)

type Status string

const (
	StatusCompliant             Status = "compliant"
	StatusCompliantWithWarnings Status = "compliant_with_warnings"
	StatusNonCompliant          Status = "non_compliant"
)

type CheckStatus string

const (
	CheckPass    CheckStatus = "pass"
	CheckWarning CheckStatus = "warning"
	CheckFail    CheckStatus = "fail"
	CheckSkipped CheckStatus = "skipped"
)

type Inputs struct {
	BranchName    string `json:"branch_name,omitempty"`
	CommitMessage string `json:"-"`
	PRBody        string `json:"-"`
	Repository    string `json:"repository,omitempty"`
	Ref           string `json:"ref,omitempty"`
	SHA           string `json:"sha,omitempty"`
	PRNumber      int    `json:"pr_number,omitempty"`
}

type Check struct {
	ID             string                    `json:"id"`
	Name           string                    `json:"name"`
	Status         CheckStatus               `json:"status"`
	Score          int                       `json:"score,omitempty"`
	Weight         int                       `json:"weight"`
	Value          string                    `json:"value,omitempty"`
	Notes          []string                  `json:"notes,omitempty"`
	Errors         []string                  `json:"errors,omitempty"`
	Warnings       []string                  `json:"warnings,omitempty"`
	FixSuggestions []validator.FixSuggestion `json:"fix_suggestions,omitempty"`
}

type Report struct {
	Title         string         `json:"title"`
	Profile       string         `json:"profile"`
	PolicyProfile string         `json:"policy_profile,omitempty"`
	Status        Status         `json:"status"`
	Score         int            `json:"score"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Repository    string         `json:"repository,omitempty"`
	Ref           string         `json:"ref,omitempty"`
	SHA           string         `json:"sha,omitempty"`
	PRNumber      int            `json:"pr_number,omitempty"`
	BranchName    string         `json:"branch_name,omitempty"`
	Checks        []Check        `json:"checks"`
	Policy        *policy.Policy `json:"policy,omitempty"`
}

type Options struct {
	Strict      bool
	GeneratedAt time.Time
	Check       string
}

// detectCIWorkflows checks if CI workflow files exist and returns their combined content.
func detectCIWorkflows() (bool, string) {
	workflowDir := ".github/workflows"
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		return false, ""
	}
	var content strings.Builder
	hasYAML := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml") {
			data, err := os.ReadFile(filepath.Join(workflowDir, e.Name()))
			if err == nil {
				content.Write(data)
				content.WriteString("\n")
				hasYAML = true
			}
		}
	}
	return hasYAML, content.String()
}

// detectCommitAuthorEmail extracts the commit author email from environment or git.
func detectCommitAuthorEmail() string {
	// Check env var first
	if email := os.Getenv("ODS_COMMIT_AUTHOR_EMAIL"); email != "" {
		return email
	}
	if email := os.Getenv("GITHUB_ACTOR"); email != "" {
		// GITHUB_ACTOR is a username, not email — but still useful for detection
		// Fall through to git
		_ = email
	}
	// Try git log for the latest commit author email
	return gitOutput("log", "-1", "--format=%ae")
}

// detectChangedFiles returns the list of files changed in the latest commit or PR.
func detectChangedFiles() []string {
	// Check env var
	if files := os.Getenv("ODS_CHANGED_FILES"); files != "" {
		return strings.Split(files, ",")
	}
	// Use git diff to get changed files
	output := gitOutput("diff", "--name-only", "HEAD~1..HEAD")
	if output == "" {
		// Try against main/master
		for _, base := range []string{"origin/main", "origin/master", "main", "master"} {
			output = gitOutput("diff", "--name-only", base+"..HEAD")
			if output != "" {
				break
			}
		}
	}
	if output == "" {
		return nil
	}
	var result []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// detectReviewData tries to extract reviewer data from the GitHub event payload
// and environment variables.
func detectReviewData() ([]string, string) {
	// Check for explicit env var overrides (for local/dev testing)
	reviewersEnv := os.Getenv("ODS_REVIEWERS")
	if reviewersEnv != "" {
		author := envFirst("ODS_PR_AUTHOR")
		return strings.Split(reviewersEnv, ","), author
	}

	path := os.Getenv("GITHUB_EVENT_PATH")
	if path == "" {
		return nil, ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}

	// Parse both pull_request and pull_request_review event types
	var event struct {
		// For pull_request_review events
		Review struct {
			User struct {
				Login string `json:"login"`
			} `json:"user"`
			State string `json:"state"`
		} `json:"review"`
		// For pull_request events (contains the PR data)
		PullRequest struct {
			User struct {
				Login string `json:"login"`
			} `json:"user"`
			RequestedReviewers []struct {
				Login string `json:"login"`
			} `json:"requested_reviewers"`
			// Approved reviews embedded in PR event
			Reviews []struct {
				User  struct {
					Login string `json:"login"`
				} `json:"user"`
				State string `json:"state"`
			} `json:"reviews"`
		} `json:"pull_request"`
		// For push events
		HeadCommit struct {
			Author struct {
				Email    string `json:"email"`
				Username string `json:"username"`
			} `json:"author"`
		} `json:"head_commit"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, ""
	}

	var reviewers []string
	seen := map[string]bool{}

	// Collect from review event
	if event.Review.User.Login != "" && event.Review.State == "approved" {
		if !seen[event.Review.User.Login] {
			reviewers = append(reviewers, event.Review.User.Login)
			seen[event.Review.User.Login] = true
		}
	}

	// Collect from PR embedded reviews (pull_request events often include recent reviews)
	for _, r := range event.PullRequest.Reviews {
		if r.State == "approved" && r.User.Login != "" && !seen[r.User.Login] {
			reviewers = append(reviewers, r.User.Login)
			seen[r.User.Login] = true
		}
	}

	author := event.PullRequest.User.Login
	if author == "" {
		author = event.HeadCommit.Author.Username
	}
	if author == "" {
		author = event.Sender.Login
	}

	return reviewers, author
}

func DiscoverInputs() Inputs {
	in := Inputs{
		BranchName: envFirst("ODS_BRANCH_NAME", "GITHUB_HEAD_REF", "GITHUB_REF_NAME"),
		Repository: envFirst("ODS_REPOSITORY", "GITHUB_REPOSITORY"),
		Ref:        envFirst("ODS_REF", "GITHUB_REF_NAME", "GITHUB_REF"),
		SHA:        envFirst("ODS_SHA", "GITHUB_SHA"),
	}

	in.CommitMessage = readEnvValue("ODS_COMMIT_MESSAGE", "ODS_COMMIT_MESSAGE_FILE")
	in.PRBody = readEnvValue("ODS_PR_BODY", "ODS_PR_BODY_FILE")
	in.PRNumber = parseEnvInt("ODS_PR_NUMBER")

	readGitHubEvent(&in)

	if in.BranchName == "" {
		in.BranchName = gitOutput("branch", "--show-current")
	}
	if in.CommitMessage == "" {
		in.CommitMessage = gitOutput("log", "-1", "--format=%B")
	}
	if in.Ref == "" {
		in.Ref = gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	}
	if in.SHA == "" {
		in.SHA = gitOutput("rev-parse", "HEAD")
	}

	return in
}

func Build(in Inputs, opts Options) Report {
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	// Load policy for report metadata
	p, err := policy.LoadPolicy()
	policyProfile := "enterprise"
	if err == nil && p != nil {
		policyProfile = p.Profile
	}

	// Build check inputs
	ci := CheckInputs{
		BranchName:    in.BranchName,
		CommitMessage: in.CommitMessage,
		PRBody:        in.PRBody,
	}

	// Try to detect CI workflows
	ci.CIWorkflowsExist, ci.CIWorkflowContent = detectCIWorkflows()
	// Try to detect review data from GitHub event
	ci.ReviewerLogins, ci.PRAuthor = detectReviewData()
	// Try to detect commit author email
	ci.CommitAuthorEmail = detectCommitAuthorEmail()
	// Detect changed files from git
	ci.ChangedFiles = detectChangedFiles()

	r := Report{
		Title:         "ODS Compliance Report",
		Profile:       "L1",
		PolicyProfile: policyProfile,
		Policy:        p,
		GeneratedAt:   generatedAt,
		Repository:    in.Repository,
		Ref:           in.Ref,
		SHA:           in.SHA,
		PRNumber:      in.PRNumber,
		BranchName:    in.BranchName,
		Checks:        buildChecks(in, ci, opts, p),
	}
	r.Score, r.Status = summarize(r.Checks)
	return r
}

func buildChecks(in Inputs, ci CheckInputs, opts Options, p *policy.Policy) []Check {
	// Determine which checks to run
	filterID := ""
	switch opts.Check {
	case "branch-naming":
		return []Check{validateBranch(in.BranchName, opts.Strict, p)}
	case "commit-message":
		return []Check{checkCommitMessage(ci, opts, p)}
	case "pr-description":
		return []Check{checkPRDescription(ci, opts, p)}
	case "":
		// Run all checks
	default:
		filterID = opts.Check
	}

	all := AllChecks(ci, opts, p)
	if filterID != "" {
		for _, c := range all {
			if c.ID == filterID {
				return []Check{c}
			}
		}
		return []Check{skipped(filterID, filterID, "check not found")}
	}
	return all
}

func WriteFiles(r Report, outputDir string) error {
	if outputDir == "" {
		outputDir = "ods-report"
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	files := map[string][]byte{}

	jsonBytes, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	files["ods-compliance.json"] = append(jsonBytes, '\n')

	md, err := Markdown(r)
	if err != nil {
		return err
	}
	files["ods-summary.md"] = []byte(md)

	page, err := HTML(r)
	if err != nil {
		return err
	}
	files["index.html"] = []byte(page)
	files["ods-compliance.svg"] = []byte(SVG(r))

	sarifBytes, err := SARIF(r)
	if err != nil {
		return err
	}
	files["ods-compliance.sarif"] = sarifBytes

	for name, body := range files {
		if err := os.WriteFile(filepath.Join(outputDir, name), body, 0644); err != nil {
			return err
		}
	}
	return nil
}

func Markdown(r Report) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "## ODS Compliance Report\n\n")
	fmt.Fprintf(&b, "Status: %s %s  \n", statusIcon(r.Status), statusLabel(r.Status))
	fmt.Fprintf(&b, "Score: %d / 100  \n", r.Score)
	policyDisplay := r.PolicyProfile
	if policyDisplay == "" {
		policyDisplay = "enterprise"
	}
	fmt.Fprintf(&b, "Policy: `%s` - %s\n", policyDisplay, policyDescription(policyDisplay))
	if r.Repository != "" || r.Ref != "" || r.SHA != "" || r.PRNumber > 0 {
		fmt.Fprintf(&b, "\n")
	}
	if r.Repository != "" {
		fmt.Fprintf(&b, "Repository: `%s`  \n", r.Repository)
	}
	if r.Ref != "" {
		fmt.Fprintf(&b, "Ref: `%s`  \n", r.Ref)
	}
	if r.SHA != "" {
		fmt.Fprintf(&b, "Commit: `%s`  \n", shortSHA(r.SHA))
	}
	if r.PRNumber > 0 {
		fmt.Fprintf(&b, "Pull request: #%d  \n", r.PRNumber)
	}
	fmt.Fprintf(&b, "\n| Check | Result | Notes |\n")
	fmt.Fprintf(&b, "|---|---|---|\n")
	for _, check := range r.Checks {
		fmt.Fprintf(&b, "| %s | %s %s | %s |\n", check.Name, checkIcon(check.Status), checkLabel(check.Status), escapeMarkdownNotes(check))
	}

	// Add fix suggestions section if any checks failed or have warnings
	hasFixes := false
	for _, check := range r.Checks {
		if len(check.FixSuggestions) > 0 && (check.Status == CheckFail || check.Status == CheckWarning) {
			hasFixes = true
			break
		}
	}
	if hasFixes {
		fmt.Fprintf(&b, "\n---\n\n## 🔧 Fix Suggestions\n\n")
		for _, check := range r.Checks {
			if len(check.FixSuggestions) == 0 || (check.Status != CheckFail && check.Status != CheckWarning) {
				continue
			}
			fmt.Fprintf(&b, "### %s\n\n", check.Name)
			for i, fs := range check.FixSuggestions {
				fmt.Fprintf(&b, "**%d. %s**\n", i+1, fs.Title)
				if fs.Description != "" {
					fmt.Fprintf(&b, "%s\n\n", fs.Description)
				}
				if fs.Template != "" {
					fmt.Fprintf(&b, "```\n%s\n```\n\n", fs.Template)
				}
			}
		}
	}

	return b.String(), nil
}

func HTML(r Report) (string, error) {
	tmpl := template.Must(template.New("report").Funcs(template.FuncMap{
		"statusLabel":       statusLabel,
		"statusIcon":        statusIcon,
		"checkLabel":          checkLabel,
		"checkIcon":           checkIcon,
		"joinNotes":           joinNotes,
		"joinNotesHTML":       joinNotesHTML,
		"shortSHA":            shortSHA,
		"hasFixSuggestions":   hasFixSuggestions,
		"fixSuggestionCount":  fixSuggestionCount,
		"formatFixTemplate":   formatFixTemplate,
		"scoreColor":          scoreColor,
		"scoreWidth":          scoreWidth,
		"add":                 func(a, b int) int { return a + b },
	}).Parse(reportHTML))
	var b bytes.Buffer
	if err := tmpl.Execute(&b, r); err != nil {
		return "", err
	}
	return b.String(), nil
}

func SVG(r Report) string {
	policyLabel := r.PolicyProfile
	if policyLabel == "" {
		policyLabel = "enterprise"
	}
	// Shorten policy label for badge
	switch policyLabel {
	case "oss":
		policyLabel = "OSS"
	case "enterprise":
		policyLabel = "ENT"
	case "regulated":
		policyLabel = "REG"
	}

	label := fmt.Sprintf("ODS-%s", policyLabel)
	statusText := statusLabel(r.Status)
	message := fmt.Sprintf("%s %d/100", statusText, r.Score)
	color := "brightgreen"
	switch r.Status {
	case StatusCompliantWithWarnings:
		color = "yellow"
	case StatusNonCompliant:
		color = "red"
	}

	// Adjust width based on message length
	labelWidth := 70
	msgWidth := len(message)*7 + 20
	totalWidth := labelWidth + msgWidth
	msgX := labelWidth

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
  <title>%s: %s</title>
  <linearGradient id="s" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>
  <g clip-path="url(#r)">
    <rect width="%d" height="20" fill="#555"/>
    <rect x="%d" width="%d" height="20" fill="%s"/>
    <rect width="%d" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
    <text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="14">%s</text>
    <text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="14">%s</text>
  </g>
</svg>
`,
		totalWidth, html.EscapeString(label), html.EscapeString(message),
		html.EscapeString(label), html.EscapeString(message),
		totalWidth,
		labelWidth,
		labelWidth, msgWidth, badgeColor(color),
		totalWidth,
		labelWidth/2, html.EscapeString(label),
		labelWidth/2, html.EscapeString(label),
		msgX+msgWidth/2, html.EscapeString(message),
		msgX+msgWidth/2, html.EscapeString(message),
	)
}

func validateBranch(value string, strict bool, p *policy.Policy) Check {
	if strings.TrimSpace(value) == "" {
		return skipped("branch-naming", "Branch naming", "branch name not detected")
	}
	result, err := validator.ValidateBranchWithPolicy(strings.TrimSpace(value), p)
	return checkFromResult("branch-naming", "Branch naming", value, result, err, strict)
}

func validateCommit(value string, strict bool, p *policy.Policy) Check {
	if strings.TrimSpace(value) == "" {
		return skipped("commit-message", "Commit message", "commit message not detected")
	}
	result, err := validator.ValidateCommitMessageWithPolicy(value, p)
	return checkFromResult("commit-message", "Commit message", firstLine(value), result, err, strict)
}

func validatePR(value string, strict bool, p *policy.Policy) Check {
	if strings.TrimSpace(value) == "" {
		return skipped("pr-description", "PR description", "PR body not detected")
	}
	result, err := validator.ValidatePRDescriptionWithPolicy(value, p)
	return checkFromResult("pr-description", "PR description", "", result, err, strict)
}

func checkFromResult(id, name, value string, result validator.Result, err error, strict bool) Check {
	weight := checkWeight[id]
	if weight == 0 {
		weight = 5 // default
	}
	check := Check{
		ID:             id,
		Name:           name,
		Weight:         weight,
		Value:          value,
		Errors:         result.Errors,
		Warnings:       result.Warnings,
		FixSuggestions: result.FixSuggestions,
	}
	if err != nil && result.Status != validator.StatusNonConformant {
		check.Errors = append(check.Errors, err.Error())
	}
	switch result.Status {
	case validator.StatusConformant:
		check.Status = CheckPass
		check.Score = weight
	case validator.StatusConformantWarnings:
		if strict {
			check.Status = CheckFail
			check.Score = 0
			check.Errors = append(check.Errors, "warnings are treated as errors in strict mode")
		} else {
			check.Status = CheckWarning
			check.Score = weight / 2
		}
	default:
		check.Status = CheckFail
		check.Score = 0
	}
	return check
}

func skipped(id, name, note string) Check {
	w := checkWeight[id]
	if w == 0 {
		w = 5
	}
	return Check{ID: id, Name: name, Weight: w, Status: CheckSkipped, Notes: []string{note}}
}

// summarize computes the weighted total score (0-100) and overall status.
// Scoring model:
//   - 10 checks with weights: Critical(10), High(7), Medium(5), Low(2)
//   - Max possible total = 2*10 + 5*7 + 2*5 + 1*2 = 20+35+10+2 = 67 → normalized to 100
//   - Each check score = weight * (pass/warning/fail multiplier)
//   - Skipped checks are excluded from normalization
func summarize(checks []Check) (int, Status) {
	totalScore := 0
	maxScore := 0
	hasFail := false
	hasWarning := false

	for _, check := range checks {
		if check.Status == CheckSkipped {
			continue
		}
		totalScore += check.Score
		maxScore += check.Weight
		if check.Status == CheckFail {
			hasFail = true
		}
		if check.Status == CheckWarning {
			hasWarning = true
		}
	}

	if maxScore == 0 {
		return 0, StatusNonCompliant
	}

	// Normalize to 0-100 scale
	score := 0
	if maxScore > 0 {
		score = totalScore * 100 / maxScore
	}

	switch {
	case hasFail:
		return score, StatusNonCompliant
	case hasWarning:
		return score, StatusCompliantWithWarnings
	default:
		return score, StatusCompliant
	}
}

func readGitHubEvent(in *Inputs) {
	path := os.Getenv("GITHUB_EVENT_PATH")
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var event struct {
		Number      int `json:"number"`
		PullRequest *struct {
			Number int    `json:"number"`
			Body   string `json:"body"`
			Head   struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
		HeadCommit *struct {
			Message string `json:"message"`
		} `json:"head_commit"`
		Repository *struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return
	}
	if event.PullRequest != nil {
		if in.PRBody == "" {
			in.PRBody = event.PullRequest.Body
		}
		if in.PRNumber == 0 {
			in.PRNumber = event.PullRequest.Number
		}
		if in.PRNumber == 0 {
			in.PRNumber = event.Number
		}
		if in.BranchName == "" {
			in.BranchName = event.PullRequest.Head.Ref
		}
		if in.SHA == "" {
			in.SHA = event.PullRequest.Head.SHA
		}
	}
	if in.CommitMessage == "" && event.HeadCommit != nil {
		in.CommitMessage = event.HeadCommit.Message
	}
	if in.Repository == "" && event.Repository != nil {
		in.Repository = event.Repository.FullName
	}
	if in.Ref == "" {
		in.Ref = event.Ref
	}
}

func envFirst(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func readEnvValue(valueName, fileName string) string {
	if value := os.Getenv(valueName); value != "" {
		return value
	}
	path := os.Getenv(fileName)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func parseEnvInt(name string) int {
	value := os.Getenv(name)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func gitOutput(args ...string) string {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstLine(value string) string {
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func shortSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func statusLabel(status Status) string {
	switch status {
	case StatusCompliant:
		return "ODS Compliant"
	case StatusCompliantWithWarnings:
		return "ODS Compliant with Warnings"
	default:
		return "ODS Non-Compliant"
	}
}

func policyDescription(profile string) string {
	switch profile {
	case policy.ProfileOSS:
		return "Open-source friendly; AI disclosure optional"
	case policy.ProfileEnterprise:
		return "Full ODS L1 enforcement with AI disclosure required"
	case policy.ProfileRegulated:
		return "Maximum compliance; tickets required, all AI rules enforced"
	default:
		return "Custom policy configuration"
	}
}

func statusIcon(status Status) string {
	switch status {
	case StatusCompliant:
		return "✅"
	case StatusCompliantWithWarnings:
		return "⚠️"
	default:
		return "❌"
	}
}

func checkLabel(status CheckStatus) string {
	switch status {
	case CheckPass:
		return "Pass"
	case CheckWarning:
		return "Warning"
	case CheckFail:
		return "Fail"
	default:
		return "Skipped"
	}
}

func checkIcon(status CheckStatus) string {
	switch status {
	case CheckPass:
		return "✅"
	case CheckWarning:
		return "⚠️"
	case CheckFail:
		return "❌"
	default:
		return "➖"
	}
}

func joinNotes(check Check) string {
	notes := make([]string, 0, len(check.Notes)+len(check.Errors)+len(check.Warnings)+1)
	if check.Value != "" {
		notes = append(notes, check.Value)
	}
	notes = append(notes, check.Notes...)
	notes = append(notes, check.Errors...)
	notes = append(notes, check.Warnings...)
	if len(notes) == 0 {
		return "-"
	}
	return strings.Join(notes, "; ")
}

func joinNotesHTML(check Check) template.HTML {
	parts := make([]string, 0)
	for _, e := range check.Errors {
		parts = append(parts, fmt.Sprintf(`<span class="err-item">%s</span>`, html.EscapeString(e)))
	}
	for _, w := range check.Warnings {
		parts = append(parts, fmt.Sprintf(`<span class="warn-item">%s</span>`, html.EscapeString(w)))
	}
	if len(parts) == 0 {
		if check.Value != "" {
			parts = append(parts, html.EscapeString(check.Value))
		} else {
			parts = append(parts, "—")
		}
	}
	return template.HTML(strings.Join(parts, "<br>"))
}

func hasFixSuggestions(check Check) bool {
	return len(check.FixSuggestions) > 0 && (check.Status == CheckFail || check.Status == CheckWarning)
}

func scoreColor(score int) string {
	switch {
	case score >= 80:
		return "#067647"
	case score >= 50:
		return "#b54708"
	default:
		return "#b42318"
	}
}

func scoreWidth(score int) string {
	return fmt.Sprintf("%d%%", score)
}

func formatFixTemplate(tmpl string) template.HTML {
	return template.HTML(fmt.Sprintf("<pre><code>%s</code></pre>", html.EscapeString(tmpl)))
}

func fixSuggestionCount(checks []Check) int {
	count := 0
	for _, c := range checks {
		count += len(c.FixSuggestions)
	}
	return count
}

func escapeMarkdownNotes(check Check) string {
	note := strings.ReplaceAll(joinNotes(check), "\n", " ")
	note = strings.ReplaceAll(note, "|", "\\|")
	return note
}

func badgeColor(color string) string {
	switch color {
	case "brightgreen":
		return "#4c1"
	case "yellow":
		return "#dfb317"
	case "red":
		return "#e05d44"
	default:
		return "#9f9f9f"
	}
}

// SARIF generates a SARIF v2.1.0 log file from the report.
// SARIF (Static Analysis Results Interchange Format) is an OASIS standard
// supported by GitHub code scanning, Azure DevOps, and other CI tools.
func SARIF(r Report) ([]byte, error) {
	log := sarifLog{
		Version: "2.1.0",
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Runs: []sarifRun{{
			Tool: sarifTool{
				Driver: sarifDriver{
					Name:           "ODS CLI",
					Version:        version.Value,
					InformationURI: "https://open-delivery-spec.github.io/spec",
					Rules:          buildSARIFRules(),
				},
			},
			Results: buildSARIFResults(r),
		}},
	}

	if r.Repository != "" {
		log.Runs[0].VersionControlProvenance = []sarifVersionControl{{
			RepositoryURI: fmt.Sprintf("https://github.com/%s", r.Repository),
			RevisionID:    r.SHA,
		}}
	}

	return json.MarshalIndent(log, "", "  ")
}

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool                       sarifTool              `json:"tool"`
	Results                    []sarifResult          `json:"results"`
	VersionControlProvenance   []sarifVersionControl  `json:"versionControlProvenance,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ShortDescription sarifMessage `json:"shortDescription"`
	HelpURI          string `json:"helpUri,omitempty"`
}

type sarifResult struct {
	RuleID    string        `json:"ruleId"`
	RuleIndex int           `json:"ruleIndex"`
	Level     string        `json:"level"`
	Message   sarifMessage  `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifVersionControl struct {
	RepositoryURI string `json:"repositoryUri"`
	RevisionID    string `json:"revisionId,omitempty"`
}

func buildSARIFRules() []sarifRule {
	return []sarifRule{
		{ID: "ODS01", Name: "AIDisclosure", ShortDescription: sarifMessage{Text: "AI code disclosure in commits and PRs"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/ai-disclosure"},
		{ID: "ODS02", Name: "HumanReviewEvidence", ShortDescription: sarifMessage{Text: "Evidence of human review on AI-generated code"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/human-review-evidence"},
		{ID: "ODS03", Name: "RequiredCI", ShortDescription: sarifMessage{Text: "CI workflow is configured and triggers on PRs"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/required-ci"},
		{ID: "ODS04", Name: "ApprovalPolicy", ShortDescription: sarifMessage{Text: "Branch protection and approval rules are configured"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/approval-policy"},
		{ID: "ODS05", Name: "AIAgentCommitDetection", ShortDescription: sarifMessage{Text: "AI agent commits are detected and flagged"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/ai-agent-commit-detection"},
		{ID: "ODS06", Name: "TestEvidence", ShortDescription: sarifMessage{Text: "Test files and test steps are present"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/test-evidence"},
		{ID: "ODS07", Name: "SecurityScanEvidence", ShortDescription: sarifMessage{Text: "Security scanning is integrated in CI"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/security-scan-evidence"},
		{ID: "ODS08", Name: "CommitMessage", ShortDescription: sarifMessage{Text: "Commit message follows Conventional Commits with AI attribution"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/commit-message"},
		{ID: "ODS09", Name: "PRDescription", ShortDescription: sarifMessage{Text: "PR description includes required sections and AI disclosure"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/pr-description"},
		{ID: "ODS10", Name: "ReleaseReadiness", ShortDescription: sarifMessage{Text: "Release process integrates ODS compliance checks"}, HelpURI: "https://open-delivery-spec.github.io/spec/checks/release-readiness"},
	}
}

func buildSARIFResults(r Report) []sarifResult {
	// Map check IDs to rule indices
	ruleIndexMap := map[string]int{
		"ai-disclosure":             0,
		"human-review-evidence":     1,
		"required-ci":               2,
		"approval-policy":           3,
		"ai-agent-commit-detection": 4,
		"test-evidence":             5,
		"security-scan-evidence":    6,
		"commit-message":            7,
		"pr-description":            8,
		"release-readiness":         9,
	}

	results := make([]sarifResult, 0, len(r.Checks))
	for _, check := range r.Checks {
		if check.Status == CheckSkipped || check.Status == CheckPass {
			continue
		}

		level := "warning"
		if check.Status == CheckFail {
			level = "error"
		}

		message := check.Name + ": " + joinNotes(check)
		if len(check.Errors) > 0 {
			message = check.Name + ": " + strings.Join(check.Errors, "; ")
		} else if len(check.Warnings) > 0 {
			message = check.Name + ": " + strings.Join(check.Warnings, "; ")
		}

		ruleIndex, ok := ruleIndexMap[check.ID]
		if !ok {
			ruleIndex = 9 // fallback
		}

		result := sarifResult{
			RuleID:    fmt.Sprintf("ODS%02d", ruleIndex+1),
			RuleIndex: ruleIndex,
			Level:     level,
			Message:   sarifMessage{Text: message},
		}

		results = append(results, result)
	}
	return results
}

const reportHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .Title }}</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1f2937;
      --muted: #6b7280;
      --line: #d1d5db;
      --ok: #067647;
      --ok-bg: #ecfdf3;
      --warn: #b54708;
      --warn-bg: #fffaeb;
      --fail: #b42318;
      --fail-bg: #fef3f2;
      --bg: #f8fafc;
      --card: #ffffff;
      --radius: 10px;
    }
    body {
      margin: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
      color: var(--ink);
      background: var(--bg);
      line-height: 1.6;
    }
    main {
      max-width: 980px;
      margin: 0 auto;
      padding: 40px 24px;
    }

    /* Header */
    header { margin-bottom: 28px; }
    h1 { font-size: 30px; margin: 0 0 6px; letter-spacing: -0.5px; font-weight: 700; }
    .meta { color: var(--muted); font-size: 14px; line-height: 1.7; }
    .meta code { background: #e5e7eb; padding: 1px 6px; border-radius: 4px; font-size: 13px; }

    /* Summary cards */
    .summary {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 14px;
      margin: 28px 0;
    }
    .metric {
      border: 1px solid var(--line);
      border-radius: var(--radius);
      padding: 18px 16px;
      background: var(--card);
      position: relative;
    }
    .metric .label {
      display: block;
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.6px;
      font-weight: 600;
      margin-bottom: 6px;
    }
    .metric .value {
      display: block;
      font-size: 22px;
      font-weight: 700;
    }
    .metric.status-ok .value { color: var(--ok); }
    .metric.status-warn .value { color: var(--warn); }
    .metric.status-fail .value { color: var(--fail); }

    /* Score gauge */
    .score-gauge {
      margin: 24px 0;
      background: var(--card);
      border: 1px solid var(--line);
      border-radius: var(--radius);
      padding: 20px 24px;
    }
    .score-gauge h3 { margin: 0 0 14px; font-size: 16px; }
    .gauge-bar {
      height: 14px;
      background: #e5e7eb;
      border-radius: 7px;
      overflow: hidden;
      position: relative;
    }
    .gauge-fill {
      height: 100%;
      border-radius: 7px;
      transition: width 0.4s ease;
    }
    .gauge-labels {
      display: flex;
      justify-content: space-between;
      margin-top: 6px;
      font-size: 12px;
      color: var(--muted);
    }

    /* Checks table */
    section.checks { margin: 28px 0; }
    section.checks h3 { margin: 0 0 14px; font-size: 16px; }
    table {
      width: 100%;
      border-collapse: collapse;
      background: var(--card);
      border: 1px solid var(--line);
      border-radius: var(--radius);
      overflow: hidden;
    }
    th, td {
      padding: 12px 16px;
      border-bottom: 1px solid var(--line);
      text-align: left;
      vertical-align: top;
      font-size: 14px;
    }
    th {
      font-size: 12px;
      color: var(--muted);
      background: #f3f4f6;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      font-weight: 600;
    }
    tr:last-child td { border-bottom: 0; }
    .pass { color: var(--ok); font-weight: 700; }
    .warning { color: var(--warn); font-weight: 700; }
    .fail { color: var(--fail); font-weight: 700; }
    .skipped { color: var(--muted); font-weight: 700; }
    .err-item { color: var(--fail); font-size: 13px; }
    .warn-item { color: var(--warn); font-size: 13px; }

    /* Fix suggestions */
    section.fixes { margin: 32px 0; }
    section.fixes h3 { margin: 0 0 18px; font-size: 18px; }
    .fix-card {
      background: var(--card);
      border: 1px solid var(--line);
      border-left: 4px solid var(--warn);
      border-radius: var(--radius);
      padding: 18px 20px;
      margin-bottom: 14px;
    }
    .fix-card.fix-error { border-left-color: var(--fail); }
    .fix-card h4 { margin: 0 0 6px; font-size: 15px; }
    .fix-card .fix-desc { color: var(--muted); font-size: 14px; margin-bottom: 10px; }
    .fix-card pre {
      background: #f3f4f6;
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 12px 14px;
      overflow-x: auto;
      font-size: 13px;
      margin: 0;
      line-height: 1.5;
    }
    .fix-card pre code { color: var(--ink); }
    .copy-btn {
      display: inline-block;
      margin-top: 8px;
      font-size: 12px;
      color: #2563eb;
      text-decoration: none;
      cursor: pointer;
      background: none;
      border: none;
      font-family: inherit;
      padding: 4px 8px;
      border-radius: 4px;
      transition: background 0.15s;
    }
    .copy-btn:hover { background: #eff6ff; }

    /* Policy info */
    .policy-info {
      margin-top: 32px;
      padding: 14px 20px;
      background: #f3f4f6;
      border-radius: var(--radius);
      font-size: 13px;
      color: var(--muted);
    }
    .policy-info strong { color: var(--ink); }

    @media (max-width: 720px) {
      .summary { grid-template-columns: repeat(2, 1fr); }
      main { padding: 20px 14px; }
      h1 { font-size: 24px; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <h1>{{ .Title }}</h1>
      <div class="meta">
        Generated <strong>{{ .GeneratedAt.Format "2006-01-02 15:04:05 MST" }}</strong>
        {{- if .Repository }} &middot; <code>{{ .Repository }}</code>{{ end }}
        {{- if .Ref }} on <code>{{ .Ref }}</code>{{ end }}
        {{- if .SHA }} at <code>{{ shortSHA .SHA }}</code>{{ end }}
        {{- if .PRNumber }} &middot; PR <strong>#{{ .PRNumber }}</strong>{{ end }}
      </div>
    </header>

    <section class="summary" aria-label="Report summary">
      <div class="metric status-{{ .Status }}">
        <span class="label">Status</span>
        <span class="value">{{ statusIcon .Status }} {{ statusLabel .Status }}</span>
      </div>
      <div class="metric">
        <span class="label">Score</span>
        <span class="value">{{ .Score }} / 100</span>
      </div>
      <div class="metric">
        <span class="label">Policy</span>
        <span class="value">{{ .PolicyProfile }}</span>
      </div>
      <div class="metric">
        <span class="label">Fix Suggestions</span>
        <span class="value">{{ fixSuggestionCount .Checks }}</span>
      </div>
    </section>

    <div class="score-gauge">
      <h3>Compliance Score</h3>
      <div class="gauge-bar">
        <div class="gauge-fill" style="width:{{ scoreWidth .Score }};background:{{ scoreColor .Score }}"></div>
      </div>
      <div class="gauge-labels">
        <span>0</span><span>25</span><span>50</span><span>75</span><span>100</span>
      </div>
    </div>

    <section class="checks">
      <h3>Check Results</h3>
      <table>
        <thead><tr><th>Check</th><th>Result</th><th>Details</th></tr></thead>
        <tbody>
          {{ range .Checks }}
          <tr>
            <td>{{ .Name }}</td>
            <td class="{{ .Status }}">{{ checkIcon .Status }} {{ checkLabel .Status }}</td>
            <td>{{ joinNotesHTML . }}</td>
          </tr>
          {{ end }}
        </tbody>
      </table>
    </section>

    {{ if gt (fixSuggestionCount .Checks) 0 }}
    <section class="fixes">
      <h3>🔧 Fix Suggestions</h3>
      <p style="color:var(--muted);font-size:14px;margin:0 0 16px">
        Copy and paste the templates below to fix the issues.
      </p>
      {{ range .Checks }}
        {{ if hasFixSuggestions . }}
          <h4 style="margin:18px 0 10px;font-size:14px;color:var(--muted)">{{ .Name }}</h4>
          {{ range $i, $fs := .FixSuggestions }}
          <div class="fix-card">
            <h4>{{ add 1 $i }}. {{ $fs.Title }}</h4>
            <div class="fix-desc">{{ $fs.Description }}</div>
            {{ if $fs.Template }}<pre><code>{{ $fs.Template }}</code></pre>{{ end }}
            <button class="copy-btn" onclick="navigator.clipboard.writeText(this.previousElementSibling.textContent.trim())">📋 Copy template</button>
          </div>
          {{ end }}
        {{ end }}
      {{ end }}
    </section>
    {{ end }}

    <div class="policy-info">
      <strong>Policy:</strong> {{ .PolicyProfile }} &middot;
      <strong>Report:</strong> {{ .Title }} &middot;
      <strong>Generated:</strong> {{ .GeneratedAt.Format "2006-01-02 15:04:05 MST" }}
    </div>
  </main>
</body>
</html>
`
