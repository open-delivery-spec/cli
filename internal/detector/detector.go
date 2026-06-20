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
	"strings"
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

// DefaultOptions returns sensible defaults.
func DefaultOptions() Options {
	return Options{
		DiffBase:   "HEAD~1",
		MaxCommits: 10,
	}
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

	// Source 4: Diff heuristics (statistical fingerprinting)
	fileDetections, diffEvidence := detectFromDiff(opts.DiffBase)
	result.Files = fileDetections
	result.Evidence = append(result.Evidence, diffEvidence...)
	if len(fileDetections) > 0 {
		result.Sources = append(result.Sources, "diff-heuristics")
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

// detectFromCommits checks recent git commits for AI-related trailers.
func detectFromCommits(opts Options) []Evidence {
	var evidence []Evidence

	log, err := gitOutput("log", fmt.Sprintf("-%d", opts.MaxCommits),
		"--format=%B", "--no-merges")
	if err != nil || log == "" {
		// Try reading commit message from file
		if opts.CommitMessageFile != "" {
			data, err := os.ReadFile(opts.CommitMessageFile)
			if err == nil {
				log = string(data)
			}
		}
	}

	if log == "" {
		return nil
	}

	// Parse each commit for AI-related footers
	// AI-assisted: true | AI-tool: <name> | Co-Authored-By: Claude <...>
	commits := strings.Split(log, "\n\n---\n\n")
	if len(commits) == 1 {
		// git log doesn't add separators; split on commit delimiters
		commits = splitCommits(log)
	}

	for i, commit := range commits {
		commit = strings.TrimSpace(commit)
		if commit == "" {
			continue
		}
		lines := strings.Split(commit, "\n")

		hasAI := false
		var aiTool, aiScope string

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
			if strings.HasPrefix(strings.ToLower(line), "ai-tool:") {
				aiTool = strings.TrimSpace(line[len("ai-tool:"):])
				hasAI = true
			}
			if strings.HasPrefix(strings.ToLower(line), "ai-scope:") {
				aiScope = strings.TrimSpace(line[len("ai-scope:"):])
			}
		}

		if hasAI {
			value := "AI-assisted commit detected"
			if aiTool != "" {
				value = fmt.Sprintf("AI-assisted commit (tool: %s)", aiTool)
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
			_ = i // commit index for future use
		}
	}

	return evidence
}

// splitCommits splits a raw git log output into individual commit messages.
func splitCommits(raw string) []string {
	// git log --format=%B produces commits separated by the record separator
	// But without --no-merges separator, commits run together.
	// Use a heuristic: look for "commit <hash>" lines or conventional commit starts
	var commits []string
	scanner := bufio.NewScanner(strings.NewReader(raw))
	var buf strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		// Detect start of a new commit by conventional commit pattern at beginning of line
		if buf.Len() > 0 && isConventionalCommitStart(line) {
			commits = append(commits, buf.String())
			buf.Reset()
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	if buf.Len() > 0 {
		commits = append(commits, buf.String())
	}
	if len(commits) <= 1 {
		return []string{raw}
	}
	return commits
}

var convCommitPattern = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\(.+\))?!?: `)

func isConventionalCommitStart(line string) bool {
	return convCommitPattern.MatchString(line)
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

// ParseCommitMessage extracts AI signals from a raw commit message string.
func ParseCommitMessage(msg string) []Evidence {
	var evidence []Evidence
	lines := strings.Split(msg, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.EqualFold(line, "AI-assisted: true") ||
			strings.EqualFold(line, "AI-generated: true") {
			evidence = append(evidence, Evidence{
				Source:     "commit-message",
				Signal:     "ai-footer",
				Value:      "AI-assisted commit",
				Confidence: 0.9,
			})
		}
		if strings.HasPrefix(strings.ToLower(line), "ai-tool:") {
			tool := strings.TrimSpace(line[len("ai-tool:"):])
			evidence = append(evidence, Evidence{
				Source:     "commit-message",
				Signal:     "ai-tool",
				Value:      fmt.Sprintf("AI tool: %s", tool),
				Confidence: 0.95,
			})
		}
		if isAICoAuthor(line) {
			tool := extractCoAuthorTool(line)
			value := "AI Co-Authored-By trailer"
			if tool != "" {
				value = fmt.Sprintf("AI Co-Authored-By: %s", tool)
			}
			evidence = append(evidence, Evidence{
				Source:     "commit-message",
				Signal:     "co-authored-by",
				Value:      value,
				Confidence: 0.9,
			})
		}
	}
	return evidence
}

// ParseDiffLines runs AI pattern heuristics on a set of code lines directly.
// This is used by the CLI when diff lines are provided explicitly.
func ParseDiffLines(lines []string) *FileDetection {
	if len(lines) == 0 {
		return nil
	}
	score := scoreAIPatterns(lines)
	return &FileDetection{
		AILines:    int(float64(len(lines)) * score),
		TotalLines: len(lines),
		Confidence: score,
	}
}

// FormatDiffLineCount returns a human-readable summary of how many lines are AI-generated.
func FormatDiffLineCount(files []FileDetection) string {
	if len(files) == 0 {
		return "0/0 lines"
	}
	totalAI := 0
	totalAll := 0
	for _, f := range files {
		totalAI += f.AILines
		totalAll += f.TotalLines
	}
	pct := 0
	if totalAll > 0 {
		pct = totalAI * 100 / totalAll
	}
	return fmt.Sprintf("%d/%d lines (~%d%%)", totalAI, totalAll, pct)
}

// CountAIFiles returns the number of files with AI-pattern matches.
func CountAIFiles(files []FileDetection) int {
	count := 0
	for _, f := range files {
		if f.Confidence >= 0.4 {
			count++
		}
	}
	return count
}

// HeuristicScores breaks down the AI score per heuristic for a set of lines.
func HeuristicScores(lines []string) map[string]float64 {
	return map[string]float64{
		"comment-ratio":            commentRatio(lines),
		"verbose-naming":           verboseNamingScore(lines),
		"redundant-error-handling": redundantErrorHandlingScore(lines),
		"uniform-indent":           uniformIndentScore(lines),
	}
}

// IsHighConfidence returns true if the detection confidence exceeds 80%.
func (r *DetectionResult) IsHighConfidence() bool {
	return r.Confidence >= 0.8
}

// IsLowConfidence returns true if the detection confidence is below 40%.
func (r *DetectionResult) IsLowConfidence() bool {
	return r.Confidence < 0.4
}

// FileCount returns the number of files with detected AI patterns.
func (r *DetectionResult) FileCount() int {
	return CountAIFiles(r.Files)
}

// LineSummary returns "ai_lines/total_lines" string.
func (r *DetectionResult) LineSummary() string {
	return FormatDiffLineCount(r.Files)
}
