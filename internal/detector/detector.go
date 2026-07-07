// Package detector provides AI code detection capabilities for git repositories.
// It analyzes commits, branch names, PR descriptions, and code diffs to determine
// whether a change contains AI-generated code — without relying on developer self-disclosure.
package detector

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/open-delivery-spec/cli/internal/gitai"
)

// DetectionResult is the output of AI code detection for a given ref range.
type DetectionResult struct {
	AIGenerated bool            `json:"ai_generated"`
	Confidence  float64         `json:"confidence"`
	Files       []FileDetection `json:"files,omitempty"`
	Evidence    []Evidence      `json:"evidence"`
	Sources     []string        `json:"sources"`
	Summary     string          `json:"summary"`
}

// FileDetection describes AI detection results for a single file.
type FileDetection struct {
	Path       string  `json:"path"`
	AILines    int     `json:"ai_lines"`
	TotalLines int     `json:"total_lines"`
	Confidence float64 `json:"confidence"`
}

// Evidence records a specific signal that contributed to the detection.
type Evidence struct {
	Source     string  `json:"source"`
	Signal     string  `json:"signal"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

// Options configures the detection behavior.
type Options struct {
	// DiffBase is the git ref to diff against (e.g., "HEAD~1", "origin/main").
	DiffBase string
	// PRBody is the pull request description text.
	PRBody string
	// BranchName is the current branch name.
	BranchName string
	// CommitMessageFile is a path to a file containing the commit message.
	CommitMessageFile string
	// MaxCommits is the maximum number of commits to analyze.
	MaxCommits int
}

// Detect runs AI code detection across all available signal sources.
func Detect(opts Options) (*DetectionResult, error) {
	if opts.MaxCommits <= 0 {
		opts.MaxCommits = 10
	}
	if opts.DiffBase == "" {
		opts.DiffBase = "HEAD~1"
	}

	result := &DetectionResult{
		Evidence: make([]Evidence, 0),
		Sources:  make([]string, 0),
	}

	// Source 1: Git commit trailers (AI-assisted: true, Co-Authored-By: Claude...)
	commitEvidence := detectFromCommits(opts)
	result.Evidence = append(result.Evidence, commitEvidence...)
	if len(commitEvidence) > 0 {
		result.Sources = append(result.Sources, "commit-trailer")
	}

	// Source 1.5: git-ai authorship notes (refs/notes/ai) — line-level
	// attribution recorded by the git-ai extension. When present this is the
	// highest-fidelity signal available: measured AI lines per file rather
	// than a confidence-based estimate.
	gitaiAttr, _ := gitai.ReadRange(opts.DiffBase, opts.MaxCommits)
	if gitaiAttr != nil {
		for _, c := range gitaiAttr.Commits {
			value := fmt.Sprintf("AI-assisted commit %s (git-ai: %d AI line(s)", c.Hash, c.AILines)
			if len(c.Agents) > 0 {
				value += ", " + strings.Join(c.Agents, ", ")
			}
			value += ")"
			result.Evidence = append(result.Evidence, Evidence{
				Source:     "git-ai-notes",
				Signal:     "authorship-log",
				Value:      value,
				Confidence: 0.95,
			})
		}
		result.Sources = append(result.Sources, "git-ai-notes")
	}

	// Source 2: Branch name prefix (ai-, claude/, copilot/, etc.)
	if opts.BranchName != "" {
		if ev := detectFromBranch(opts.BranchName); ev != nil {
			result.Evidence = append(result.Evidence, *ev)
			result.Sources = append(result.Sources, "branch-name")
		}
	}

	// Source 3: PR body disclosure section
	if opts.PRBody != "" {
		if ev := detectFromPRBody(opts.PRBody); ev != nil {
			result.Evidence = append(result.Evidence, *ev)
			result.Sources = append(result.Sources, "pr-body")
		}
	}

	// Source 4: per-file AI line counts. git-ai's measured authorship wins
	// over the statistical diff heuristics — never mix a measurement with a
	// guess for the same quantity.
	if gitaiAttr != nil && len(gitaiAttr.Files) > 0 {
		result.Files = filesFromGitAI(gitaiAttr, opts.DiffBase)
	} else {
		fileDetections, diffEvidence := detectFromDiff(opts.DiffBase)
		result.Files = fileDetections
		result.Evidence = append(result.Evidence, diffEvidence...)
		if len(fileDetections) > 0 {
			result.Sources = append(result.Sources, "diff-heuristics")
		}
	}

	// Aggregate: compute overall AI detection and confidence
	result.aggregate()

	return result, nil
}

// aggregate computes the overall detection verdict from collected evidence.
// Uses max-confidence as the baseline with a small boost per additional corroborating
// signal. This ensures that adding more positive signals never reduces overall
// confidence (which a weighted average does when mixing high and low-confidence signals).
func (r *DetectionResult) aggregate() {
	if len(r.Evidence) == 0 && len(r.Files) == 0 {
		r.AIGenerated = false
		r.Confidence = 0.0
		r.Summary = "No AI code detected"
		return
	}

	// Use the highest individual signal confidence as the baseline.
	maxConf := 0.0
	for _, ev := range r.Evidence {
		if ev.Confidence > maxConf {
			maxConf = ev.Confidence
		}
	}
	for _, f := range r.Files {
		if f.Confidence > maxConf {
			maxConf = f.Confidence
		}
	}

	// Each additional corroborating signal adds a 5% boost (capped at 1.0).
	extraSignals := len(r.Evidence) + len(r.Files) - 1
	if extraSignals > 0 {
		maxConf += 0.05 * float64(extraSignals)
		if maxConf > 1.0 {
			maxConf = 1.0
		}
	}
	r.Confidence = maxConf

	r.AIGenerated = r.Confidence >= 0.3

	if r.AIGenerated {
		fileCount := len(r.Files)
		if fileCount == 0 {
			r.Summary = fmt.Sprintf("AI code detected (confidence: %.0f%%)", r.Confidence*100)
		} else {
			totalAI := 0
			totalAll := 0
			for _, f := range r.Files {
				totalAI += f.AILines
				totalAll += f.TotalLines
			}
			r.Summary = fmt.Sprintf("AI code detected in %d file(s) — %d/%d lines (confidence: %.0f%%)",
				fileCount, totalAI, totalAll, r.Confidence*100)
		}
	} else {
		r.Summary = fmt.Sprintf("No AI code detected (confidence: %.0f%%)", r.Confidence*100)
	}
}

// knownAICoAuthorPrefixes contains lowercase name prefixes for known AI tools
// that appear as the name part of Co-Authored-By git trailers.
// Per ODS spec Module 02, Co-Authored-By is the primary AI attribution signal.
var knownAICoAuthorPrefixes = []string{
	"claude",         // Co-Authored-By: Claude <noreply@anthropic.com>  (Claude Code)
	"github copilot", // Co-Authored-By: GitHub Copilot <copilot@github.com>
	"copilot",        // Co-Authored-By: copilot[bot] <...>  (variant)
	"cursor",         // Co-Authored-By: cursor[bot] <cursor@cursor.sh>  (Cursor)
	"codeium",        // Co-Authored-By: Codeium <noreply@codeium.com>
	"tabnine",        // Co-Authored-By: tabnine[bot] <...>  (Tabnine)
	"ai",             // Co-Authored-By: AI  (generic / legacy ODS format)
}

// isAICoAuthor returns true if line is a Co-Authored-By trailer referencing a known AI tool.
func isAICoAuthor(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if !strings.HasPrefix(lower, "co-authored-by:") {
		return false
	}
	namePart := strings.TrimSpace(lower[len("co-authored-by:"):])
	for _, prefix := range knownAICoAuthorPrefixes {
		if strings.HasPrefix(namePart, prefix) {
			return true
		}
	}
	return false
}

// AITrailerTool reports the AI tool attributed in a commit message via its
// Co-Authored-By or ODS (`AI-tool:`, `AI-assisted: true`) trailers. It returns
// the tool's display name (e.g. "Claude", "GitHub Copilot"), the generic "AI"
// when attribution is present but unnamed, or "" when the message shows no AI
// attribution. This is the building block for AI-vs-human reporting.
func AITrailerTool(message string) string {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if isAICoAuthor(line) {
			if tool := extractCoAuthorTool(line); tool != "" {
				return tool
			}
			return "AI"
		}
		// Linux kernel convention (Assisted-by: AGENT:MODEL [tools...]).
		// Aggregate by agent name; the model version stays in evidence detail.
		if agent, _, ok := parseAssistedBy(line); ok {
			return agent
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "ai-tool:") {
			if tool := strings.TrimSpace(line[len("ai-tool:"):]); tool != "" {
				return tool
			}
			return "AI"
		}
		if strings.EqualFold(line, "ai-assisted: true") || strings.EqualFold(line, "ai-generated: true") {
			return "AI"
		}
	}
	return ""
}

// parseAssistedBy parses the Linux kernel's coding-assistant attribution
// trailer (Documentation/process/coding-assistants.rst):
//
//	Assisted-by: AGENT_NAME:MODEL_VERSION [TOOL1] [TOOL2]
//
// It returns the agent name and model version ("" when the writer omitted
// the :MODEL part, which happens in practice). The optional trailing
// analysis-tool list (coccinelle, sparse, ...) is not attribution and is
// ignored here.
func parseAssistedBy(line string) (agent, model string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(strings.ToLower(trimmed), "assisted-by:") {
		return "", "", false
	}
	rest := strings.TrimSpace(trimmed[len("assisted-by:"):])
	if rest == "" {
		return "", "", false
	}
	agent, model, _ = strings.Cut(strings.Fields(rest)[0], ":")
	return agent, model, agent != ""
}

// extractCoAuthorTool extracts the display name from a Co-Authored-By trailer.
func extractCoAuthorTool(line string) string {
	lower := strings.ToLower(strings.TrimSpace(line))
	if !strings.HasPrefix(lower, "co-authored-by:") {
		return ""
	}
	name := strings.TrimSpace(line[len("co-authored-by:"):])
	if idx := strings.Index(name, "<"); idx != -1 {
		name = strings.TrimSpace(name[:idx])
	}
	return name
}

// commitRecord is one scanned commit: its short hash (empty when the message
// came from a file rather than git history) and full message.
type commitRecord struct {
	hash    string
	message string
}

// filesFromGitAI converts git-ai's measured per-file AI line counts into
// FileDetections. TotalLines comes from the diff's insertions per file (same
// range as the rest of detection); AI lines are capped at the insertions so
// authorship recorded on lines outside this change cannot inflate the ratio.
// Files absent from the diff are skipped for the same reason.
func filesFromGitAI(attr *gitai.RangeAttribution, base string) []FileDetection {
	insertions := map[string]int{}
	if out, err := gitOutput("diff", "--numstat", base); err == nil {
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			if n, err := strconv.Atoi(fields[0]); err == nil { // "-" for binary
				insertions[fields[2]] = n
			}
		}
	}

	paths := make([]string, 0, len(attr.Files))
	for p := range attr.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var files []FileDetection
	for _, path := range paths {
		total, changed := insertions[path]
		if !changed || total == 0 {
			continue
		}
		ai := attr.Files[path]
		if ai > total {
			ai = total
		}
		files = append(files, FileDetection{
			Path:       path,
			AILines:    ai,
			TotalLines: total,
			Confidence: 0.95,
		})
	}
	return files
}

// detectFromCommits checks the commits under review for AI-related trailers.
//
// The scan is scoped to DiffBase..HEAD: only commits that are part of the
// change under review are attribution evidence for it. An unbounded
// `git log -N` walks past the base into the target branch's history, where
// previously merged AI-attributed commits would be misattributed to the
// current change — on a repo whose main history contains AI commits, a purely
// human PR would flag as AI-generated. MaxCommits stays as a cap; when the
// base ref does not resolve (shallow clone, initial commit) the scan falls
// back to the unscoped window rather than reporting nothing.
func detectFromCommits(opts Options) []Evidence {
	var evidence []Evidence

	// %x1e/%x1f: record/field separators that cannot appear in git hashes and
	// are vanishingly unlikely in commit messages — no splitting heuristics.
	args := []string{"log", fmt.Sprintf("-%d", opts.MaxCommits),
		"--format=%x1e%h%x1f%B", "--no-merges"}
	if opts.DiffBase != "" {
		if _, err := gitOutput("rev-parse", "--verify", opts.DiffBase); err == nil {
			args = append(args, opts.DiffBase+"..HEAD")
		}
	}

	var records []commitRecord
	log, err := gitOutput(args...)
	if err == nil && log != "" {
		for _, rec := range strings.Split(log, "\x1e") {
			if strings.TrimSpace(rec) == "" {
				continue
			}
			parts := strings.SplitN(rec, "\x1f", 2)
			if len(parts) < 2 {
				continue
			}
			records = append(records, commitRecord{
				hash:    strings.TrimSpace(parts[0]),
				message: parts[1],
			})
		}
	} else if opts.CommitMessageFile != "" {
		// Fall back to a message file (e.g. commit-msg hook) — no hash known.
		if data, err := os.ReadFile(opts.CommitMessageFile); err == nil {
			records = append(records, commitRecord{message: string(data)})
		}
	}

	for _, rec := range records {
		commit := strings.TrimSpace(rec.message)
		if commit == "" {
			continue
		}
		lines := strings.Split(commit, "\n")

		hasAI := false
		var aiTool, aiModel, aiScope string

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.EqualFold(line, "AI-assisted: true") ||
				strings.EqualFold(line, "AI-generated: true") ||
				isAICoAuthor(line) {
				hasAI = true
				if tool := extractCoAuthorTool(line); tool != "" && aiTool == "" {
					aiTool = tool
				}
			}
			// Linux kernel convention: Assisted-by: AGENT:MODEL [tools...]
			if agent, model, ok := parseAssistedBy(line); ok {
				hasAI = true
				if aiTool == "" {
					aiTool = agent
				}
				if aiModel == "" {
					aiModel = model
				}
			}
			if strings.HasPrefix(strings.ToLower(line), "ai-tool:") {
				aiTool = strings.TrimSpace(line[len("ai-tool:"):])
				hasAI = true
			}
			if strings.HasPrefix(strings.ToLower(line), "ai-scope:") {
				aiScope = strings.TrimSpace(line[len("ai-scope:"):])
			}
		}

		if hasAI {
			// Include the short hash so each evidence row is auditable and
			// N attributed commits don't render as N identical lines.
			value := "AI-assisted commit"
			if rec.hash != "" {
				value += " " + rec.hash
			}
			if aiTool != "" {
				value += fmt.Sprintf(" (tool: %s", aiTool)
				if aiModel != "" {
					value += fmt.Sprintf(", model: %s", aiModel)
				}
				value += ")"
			}
			if aiScope != "" {
				value += fmt.Sprintf(" [scope: %s]", aiScope)
			}
			evidence = append(evidence, Evidence{
				Source:     "commit-trailer",
				Signal:     "ai-footer",
				Value:      value,
				Confidence: 0.9,
			})
		}
	}

	return evidence
}

// knownAIToolBranchNames contains the first path segment used by AI coding tools
// when they auto-create branches (e.g., claude/..., copilot/..., cursor/...).
var knownAIToolBranchNames = []string{
	"claude",  // Claude Code CLI: claude/<description>
	"copilot", // GitHub Copilot: copilot/<description>
	"cursor",  // Cursor: cursor/<description>
	"codeium", // Codeium: codeium/<description>
}

// detectFromBranch checks if the branch name indicates AI-generated code.
func detectFromBranch(branchName string) *Evidence {
	name := strings.TrimSpace(branchName)
	lower := strings.ToLower(name)

	// Check for ai- prefix at root level
	if strings.HasPrefix(lower, "ai-") {
		return &Evidence{
			Source:     "branch-name",
			Signal:     "ai-prefix",
			Value:      fmt.Sprintf("Branch '%s' has AI prefix", name),
			Confidence: 0.5,
		}
	}

	// Check first segment for known AI tool names (e.g., claude/..., copilot/...)
	segments := strings.Split(name, "/")
	firstSegmentLower := strings.ToLower(segments[0])
	for _, aiTool := range knownAIToolBranchNames {
		if firstSegmentLower == aiTool {
			return &Evidence{
				Source:     "branch-name",
				Signal:     "ai-tool-branch",
				Value:      fmt.Sprintf("Branch '%s' was created by AI tool (%s)", name, segments[0]),
				Confidence: 0.6,
			}
		}
	}

	// Check for /ai- in any branch segment
	for _, segment := range segments {
		if strings.HasPrefix(strings.ToLower(segment), "ai-") {
			return &Evidence{
				Source:     "branch-name",
				Signal:     "ai-prefix-segment",
				Value:      fmt.Sprintf("Branch '%s' has AI-prefixed segment", name),
				Confidence: 0.35,
			}
		}
	}

	return nil
}

// detectFromPRBody checks the PR description for AI disclosure.
func detectFromPRBody(body string) *Evidence {
	if body == "" {
		return nil
	}

	lower := strings.ToLower(body)

	// Pattern 1: AI Disclosure checkbox ticked
	aiCheckboxPatterns := []string{
		"[x] this pr contains ai-generated code",
		"[x] this pr contains ai generated code",
		"[x] this pr contains ai-assisted code",
		"[x] ai-generated",
		"[x] ai generated",
		"[x] ai-assisted",
		"- [x] this pr contains ai",
	}
	for _, pattern := range aiCheckboxPatterns {
		if strings.Contains(lower, pattern) {
			return &Evidence{
				Source:     "pr-body",
				Signal:     "ai-disclosure-checkbox",
				Value:      "AI disclosure checkbox is checked",
				Confidence: 0.85,
			}
		}
	}

	// Pattern 2: AI Disclosure section with explicit statement
	aiDisclosureStatements := []string{
		"ai-generated code",
		"ai generated code",
		"ai-assisted code",
		"ai tool:",
		"**ai tool:**",
		"ai-tool:",
	}
	for _, stmt := range aiDisclosureStatements {
		if strings.Contains(lower, stmt) {
			return &Evidence{
				Source:     "pr-body",
				Signal:     "ai-disclosure-text",
				Value:      fmt.Sprintf("AI disclosure text found: '%s'", stmt),
				Confidence: 0.8,
			}
		}
	}

	return nil
}

// detectFromDiff analyzes git diff output for AI code fingerprint patterns.
func detectFromDiff(base string) ([]FileDetection, []Evidence) {
	// Get changed files
	filesOutput, err := gitOutput("diff", "--name-only", base)
	if err != nil || filesOutput == "" {
		return nil, nil
	}

	changedFiles := nonEmptyLines(filesOutput)

	// Filter to code files only
	var codeFiles []string
	for _, f := range changedFiles {
		if isCodeFile(f) {
			codeFiles = append(codeFiles, f)
		}
	}

	if len(codeFiles) == 0 {
		return nil, nil
	}

	var fileDetections []FileDetection
	var diffEvidence []Evidence
	totalAIFiles := 0

	for _, file := range codeFiles {
		// Get the diff for this specific file (added lines only)
		diffOutput, err := gitOutput("diff", base, "--", file)
		if err != nil || diffOutput == "" {
			continue
		}

		addedLines := extractAddedLines(diffOutput)
		if len(addedLines) == 0 {
			continue
		}

		// Run heuristics on the added lines
		score := scoreAIPatterns(addedLines)
		if score >= 0.4 {
			aiLines := int(float64(len(addedLines)) * score)
			fileDetections = append(fileDetections, FileDetection{
				Path:       file,
				AILines:    aiLines,
				TotalLines: len(addedLines),
				Confidence: score,
			})
			totalAIFiles++
		}
	}

	if totalAIFiles > 0 {
		diffEvidence = append(diffEvidence, Evidence{
			Source:     "diff-heuristics",
			Signal:     "ai-code-patterns",
			Value:      fmt.Sprintf("%d file(s) match AI code patterns", totalAIFiles),
			Confidence: 0.4,
		})
	}

	return fileDetections, diffEvidence
}

// scoreAIPatterns runs multiple heuristics on code lines and returns a 0-1 AI likelihood score.
func scoreAIPatterns(lines []string) float64 {
	if len(lines) == 0 {
		return 0
	}

	var scores []float64

	// Heuristic 1: Comment-to-code ratio
	scores = append(scores, commentRatio(lines))

	// Heuristic 2: Verbose variable naming
	scores = append(scores, verboseNamingScore(lines))

	// Heuristic 3: Redundant error handling patterns
	scores = append(scores, redundantErrorHandlingScore(lines))

	// Heuristic 4: Overly uniform indentation (AI is more mechanically consistent)
	scores = append(scores, uniformIndentScore(lines))

	// Weighted average
	weights := []float64{0.3, 0.25, 0.25, 0.2}
	total := 0.0
	for i, s := range scores {
		total += s * weights[i]
	}

	return total
}

// commentRatio checks if the comment-to-code ratio is unusually high (>35%).
func commentRatio(lines []string) float64 {
	codeLines := 0
	commentLines := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") ||
			strings.HasPrefix(trimmed, "*/") {
			commentLines++
		} else {
			codeLines++
		}
	}

	total := commentLines + codeLines
	if total == 0 {
		return 0
	}

	ratio := float64(commentLines) / float64(total)
	// Above 35% comment ratio is suspicious for AI
	if ratio > 0.5 {
		return 0.9
	} else if ratio > 0.35 {
		return 0.6
	} else if ratio > 0.25 {
		return 0.3
	}
	return 0
}

// verboseNamingScore checks for excessively long variable names (AI pattern).
func verboseNamingScore(lines []string) float64 {
	longVarPattern := regexp.MustCompile(`\b[a-z][a-zA-Z0-9_]{20,}\b`)
	totalVars := 0
	longVars := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		matches := longVarPattern.FindAllString(line, -1)
		longVars += len(matches)

		// Count total identifiers roughly
		words := regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*\b`).FindAllString(line, -1)
		totalVars += len(words)
	}

	if totalVars == 0 {
		return 0
	}

	ratio := float64(longVars) / float64(totalVars)
	if ratio > 0.15 {
		return 0.7
	} else if ratio > 0.08 {
		return 0.4
	}
	return 0
}

// redundantErrorHandlingScore detects AI's tendency to over-handle errors.
func redundantErrorHandlingScore(lines []string) float64 {
	// Look for patterns like:
	// if err != nil { return err }
	// if err != nil { log...; return err }
	// All within a short span — AI over-defends
	errReturnPattern := regexp.MustCompile(`if err != nil \{`)
	errBlocks := 0
	totalLines := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		totalLines++
		if errReturnPattern.MatchString(trimmed) {
			errBlocks++
		}
	}

	if totalLines == 0 {
		return 0
	}

	ratio := float64(errBlocks) / float64(totalLines)
	// More than 15% of lines being error-check blocks is suspicious
	if ratio > 0.2 {
		return 0.8
	} else if ratio > 0.12 {
		return 0.5
	} else if ratio > 0.08 {
		return 0.25
	}
	return 0
}

// uniformIndentScore checks for mechanically uniform indentation patterns.
func uniformIndentScore(lines []string) float64 {
	indentCounts := make(map[int]int)
	totalIndented := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent > 0 {
			indentCounts[indent]++
			totalIndented++
		}
	}

	if totalIndented < 3 {
		return 0
	}

	// Find the most common indent level
	maxCount := 0
	for _, count := range indentCounts {
		if count > maxCount {
			maxCount = count
		}
	}

	// If >70% of indented lines use the same indent level, suspicious
	uniformity := float64(maxCount) / float64(totalIndented)
	if uniformity > 0.85 {
		return 0.7
	} else if uniformity > 0.7 {
		return 0.4
	}
	return 0
}

// extractAddedLines extracts lines prefixed with '+' from a unified diff.
func extractAddedLines(diff string) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(diff))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "++"+"+") {
			lines = append(lines, line[1:])
		}
	}
	return lines
}

// isCodeFile returns true if the file extension suggests a code file.
func isCodeFile(path string) bool {
	codeExts := map[string]bool{
		".go": true, ".rs": true, ".py": true, ".js": true, ".ts": true,
		".tsx": true, ".jsx": true, ".java": true, ".kt": true, ".swift": true,
		".c": true, ".cpp": true, ".h": true, ".hpp": true, ".cs": true,
		".rb": true, ".php": true, ".scala": true, ".clj": true, ".ex": true,
		".exs": true, ".elm": true, ".hs": true, ".ml": true, ".mli": true,
		".vue": true, ".svelte": true,
	}
	for ext := range codeExts {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

// gitOutput runs a git command and returns trimmed output.
func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// nonEmptyLines splits text by newlines and returns non-empty trimmed lines.
func nonEmptyLines(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
