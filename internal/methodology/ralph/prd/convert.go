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

// ConvertMarkdownToPRD turns a Samuel PRD markdown file (+ optional
// tasks file) into a fully-formed AutoPRD ready to be saved as
// prd.toon. Matches v1's converter so generate-tasks fixtures keep
// working.
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
		tbody, err := os.ReadFile(tasksPath)
		if err != nil {
			return nil, fmt.Errorf("read tasks file: %w", err)
		}
		tasks, err := ParseTaskMarkdown(string(tbody))
		if err != nil {
			return nil, fmt.Errorf("parse tasks: %w", err)
		}
		p.Tasks = tasks
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
