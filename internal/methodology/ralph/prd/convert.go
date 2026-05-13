package prd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Task line regex - matches v1 format with optional indent prefix:
//
//	"- [ ] 1.0 Task title [~3,000 tokens - Medium]"
//
// Groups: indent, checkbox, taskID, title, complexity.
var taskLineRegex = regexp.MustCompile(
	`^(\s*)- \[([ xX])\]\s*(\d+\.\d+|\d+)\s+(.+?)(?:\s*\[~[\d,]+\s+tokens?\s*-\s*(\w+)\])?\s*$`,
)

// prdTitleRegex grabs the first H1 from a PRD markdown file.
var prdTitleRegex = regexp.MustCompile(`^#\s+(.+)$`)

// taskHeadingRegex matches H3 headings shaped like "### N.M Title" or
// "### N Title" — the convention common to PRDs that embed tasks
// directly under a "## Tasks" section rather than in a companion
// checklist file. Groups: taskID, title.
var taskHeadingRegex = regexp.MustCompile(`^###\s+(\d+(?:\.\d+)*)\s+(.+?)\s*$`)

// taskSectionRegex matches the H2 heading that introduces a tasks
// section. Case-insensitive on the keyword; "## Tasks", "## Implementation",
// "## Steps", and "## Work Items" all qualify. Anything else under H2
// closes the tasks-section scope, so non-task subheadings don't get
// picked up as tasks.
var taskSectionRegex = regexp.MustCompile(`(?i)^##\s+(tasks|implementation(\s+plan)?|steps|work\s+items)\s*$`)

// ConvertMarkdownToPRD turns a Samuel PRD markdown file (+ optional
// tasks file) into a fully-formed AutoPRD ready to be saved as
// prd.toon.
//
// Task source resolution, in order:
//
//  1. If tasksPath is non-empty and the file contains v1-style
//     checklist entries (`- [ ] 1.1 Title`), those win — preserves
//     backward compatibility with generate-tasks fixtures.
//  2. Otherwise, scan the PRD body for `### N.M Title` headings
//     under any `## Tasks` (or `## Implementation`, `## Steps`,
//     `## Work Items`) section. This is the convention most PRDs
//     written in 2026+ actually use, and the format that the
//     `samuel run init --prd` examples in the docs show.
//
// Returns an AutoPRD with `len(p.Tasks) == 0` when neither path
// finds tasks — the caller (CLI) is responsible for surfacing
// that as a user-visible warning rather than letting the loop
// silently start with no work to do.
func ConvertMarkdownToPRD(prdPath, tasksPath string) (*AutoPRD, error) {
	body, err := os.ReadFile(prdPath)
	if err != nil {
		return nil, fmt.Errorf("read PRD: %w", err)
	}
	name, description := extractPRDMetadata(string(body))
	p := NewAutoPRD(name, description)
	p.Project.SourcePRD = prdPath
	p.Project.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	if tasksPath != "" {
		tbody, terr := os.ReadFile(tasksPath)
		if terr != nil {
			return nil, fmt.Errorf("read tasks file: %w", terr)
		}
		// ParseTaskMarkdown returns an error when the file contains
		// no checklist lines. Treat that as "fall through to the
		// inline-heading parser", not as a fatal error — the user
		// might have written tasks inline and named a stub
		// companion file by accident.
		if tasks, perr := ParseTaskMarkdown(string(tbody)); perr == nil {
			p.Tasks = tasks
		}
	}
	if len(p.Tasks) == 0 {
		p.Tasks = ParseTasksFromPRDBody(string(body))
	}
	p.RecalculateProgress()
	return p, nil
}

func extractPRDMetadata(content string) (name, description string) {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := prdTitleRegex.FindStringSubmatch(trimmed); m != nil {
			name = slugify(m[1])
			description = m[1]
			return
		}
	}
	return "unnamed-project", "Converted from PRD"
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '-' || r == '_':
			return '-'
		}
		return -1
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// ParseTasksFromPRDBody scans a PRD markdown body for tasks written
// inline as H3 headings under an H2 tasks section. Recognized shapes:
//
//	## Tasks                # or "## Implementation", "## Steps", etc.
//
//	### 1.1 Render the dry-run prompt
//
//	The loop should produce a context bundle …
//
//	**Acceptance**: prompt rendered, exit 0.
//
//	### 1.2 Honor the iteration cap
//	…
//
// Each `### N.M Title` becomes one AutoTask:
//   - ID    = N.M
//   - Title = the heading text after the ID
//   - Description = the markdown between this heading and the next
//     `### …` or `## …` heading (trimmed)
//   - ParentID inferred from N.M (parent of 1.1 is "1")
//   - Status pending, Priority/Complexity medium
//
// Returns nil (not an error) when no tasks section or no numbered
// headings are present. The caller decides what to do about empty
// results.
func ParseTasksFromPRDBody(content string) []AutoTask {
	lines := strings.Split(content, "\n")
	inTasksSection := false
	var tasks []AutoTask
	var current *AutoTask
	var descBuf []string

	flush := func() {
		if current == nil {
			return
		}
		current.Description = strings.TrimSpace(strings.Join(descBuf, "\n"))
		tasks = append(tasks, *current)
		current = nil
		descBuf = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")

		// H2 boundary: opens or closes the tasks-section scope.
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			inTasksSection = taskSectionRegex.MatchString(trimmed)
			continue
		}
		// H1 always closes the tasks section.
		if strings.HasPrefix(trimmed, "# ") {
			flush()
			inTasksSection = false
			continue
		}
		if !inTasksSection {
			continue
		}
		if m := taskHeadingRegex.FindStringSubmatch(trimmed); m != nil {
			flush()
			id, title := m[1], strings.TrimSpace(m[2])
			current = &AutoTask{
				ID:         id,
				Title:      title,
				Status:     StatusPending,
				Priority:   PriorityMedium,
				Complexity: ComplexityMedium,
				ParentID:   parentIDFrom(id),
				Source:     SourcePRD,
			}
			continue
		}
		if current != nil {
			descBuf = append(descBuf, line)
		}
	}
	flush()
	return tasks
}

// parentIDFrom returns the dotted-parent of a task ID, or "" when the
// ID has no parent (top-level "1", "2", etc.). "1.1" → "1"; "1.2.3" → "1.2".
func parentIDFrom(id string) string {
	idx := strings.LastIndex(id, ".")
	if idx <= 0 {
		return ""
	}
	return id[:idx]
}

// ParseTaskMarkdown turns the generate-tasks Markdown checklist into
// AutoTask values, preserving the indent → parent/child relationship.
func ParseTaskMarkdown(content string) ([]AutoTask, error) {
	var tasks []AutoTask
	var currentParent string
	for _, line := range strings.Split(content, "\n") {
		t, ok := parseTaskLine(line)
		if !ok {
			continue
		}
		if isChildTask(line) {
			t.ParentID = currentParent
			if currentParent != "" {
				t.DependsOn = []string{currentParent}
			}
		} else {
			currentParent = t.ID
		}
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks found in markdown")
	}
	return tasks, nil
}

func parseTaskLine(line string) (AutoTask, bool) {
	m := taskLineRegex.FindStringSubmatch(line)
	if m == nil {
		return AutoTask{}, false
	}
	status := StatusPending
	if m[2] == "x" || m[2] == "X" {
		status = StatusCompleted
	}
	complexity := strings.ToLower(strings.TrimSpace(m[5]))
	if !validComplexity(complexity) {
		complexity = ComplexityMedium
	}
	return AutoTask{
		ID:         m[3],
		Title:      strings.TrimSpace(m[4]),
		Status:     status,
		Complexity: complexity,
		Priority:   PriorityMedium,
	}, true
}

func isChildTask(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}

func validComplexity(c string) bool {
	switch c {
	case ComplexitySimple, ComplexityMedium, ComplexityComplex:
		return true
	}
	return false
}

// FindTasksFile applies the convention "tasks-<prd-base>" to locate the
// companion tasks file for prdPath. Returns "" when none exists.
func FindTasksFile(prdPath string) string {
	dir := filepath.Dir(prdPath)
	base := filepath.Base(prdPath)
	candidate := filepath.Join(dir, "tasks-"+base)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
