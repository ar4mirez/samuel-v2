package context

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
)

// Progress context tuning — defaults match v1.
const (
	DefaultMaxLearnings = 50
	DefaultMaxCompleted = 10
	DefaultMaxLines     = 500
)

// ProgressConfig controls how progress-context.md is generated. Values
// of zero fall back to defaults.
type ProgressConfig struct {
	MaxLearnings int
	MaxCompleted int
	MaxLines     int
}

// FromAutoConfig builds a ProgressConfig from an AutoConfig.
func FromAutoConfig(c prd.AutoConfig) ProgressConfig {
	pc := ProgressConfig{
		MaxLearnings: DefaultMaxLearnings,
		MaxCompleted: DefaultMaxCompleted,
		MaxLines:     DefaultMaxLines,
	}
	if c.ProgressMaxLearnings > 0 {
		pc.MaxLearnings = c.ProgressMaxLearnings
	}
	if c.ProgressMaxCompleted > 0 {
		pc.MaxCompleted = c.ProgressMaxCompleted
	}
	if c.ProgressMaxLines > 0 {
		pc.MaxLines = c.ProgressMaxLines
	}
	return pc
}

// GenerateProgressContext reads progress.md and writes
// progress-context.md alongside it. Both live in .samuel/run/.
func GenerateProgressContext(projectDir string, cfg ProgressConfig) error {
	run := prd.RunPath(projectDir)
	progressPath := filepath.Join(run, prd.ProgressFile)
	contextPath := filepath.Join(run, prd.ProgressContextFile)
	lines, err := readAllLines(progressPath)
	if err != nil {
		if os.IsNotExist(err) {
			return writeEmptyContext(contextPath)
		}
		return fmt.Errorf("read progress.md: %w", err)
	}
	if len(lines) == 0 {
		return writeEmptyContext(contextPath)
	}

	if cfg.MaxLearnings == 0 {
		cfg.MaxLearnings = DefaultMaxLearnings
	}
	if cfg.MaxCompleted == 0 {
		cfg.MaxCompleted = DefaultMaxCompleted
	}
	learnings := extractEntries(lines, "LEARNING:", cfg.MaxLearnings)
	completed := extractEntries(lines, "COMPLETED:", cfg.MaxCompleted)
	explored := extractExploredAreas(lines)
	iters, completions := countProgressStats(lines)

	return writeContextFile(contextPath, iters, completions, learnings, completed, explored)
}

// RotateProgressIfNeeded archives older progress entries when
// progress.md exceeds maxLines.
func RotateProgressIfNeeded(projectDir string, maxLines int) error {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	progressPath := filepath.Join(prd.RunPath(projectDir), prd.ProgressFile)
	lines, err := readAllLines(progressPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read progress.md: %w", err)
	}
	if len(lines) <= maxLines {
		return nil
	}
	keepFrom := len(lines) - maxLines/2
	archive := lines[:keepFrom]
	keep := lines[keepFrom:]

	archivePath := filepath.Join(prd.RunPath(projectDir),
		fmt.Sprintf("progress-archive-%s.md", time.Now().UTC().Format("20060102-150405")))
	if err := os.WriteFile(archivePath, []byte(strings.Join(archive, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return os.WriteFile(progressPath, []byte(strings.Join(keep, "\n")+"\n"), 0o644)
}

// PrepareProgressContext is the convenience wrapper the loop calls
// once per iteration: regenerate context + rotate if needed.
func PrepareProgressContext(projectDir string, cfg ProgressConfig) {
	_ = GenerateProgressContext(projectDir, cfg)
	maxLines := cfg.MaxLines
	if maxLines == 0 {
		maxLines = DefaultMaxLines
	}
	_ = RotateProgressIfNeeded(projectDir, maxLines)
}

func writeContextFile(path string, iters, completions int, learnings, completed, explored []string) error {
	var sb strings.Builder
	sb.WriteString("# Progress Context (auto-generated — do not edit)\n")
	fmt.Fprintf(&sb, "Summary: %d iterations completed, %d tasks done\n", iters, completions)
	if len(learnings) > 0 {
		sb.WriteString("\n## Key Learnings\n")
		for _, l := range learnings {
			sb.WriteString(l + "\n")
		}
	}
	if len(completed) > 0 {
		sb.WriteString("\n## Recent Completions\n")
		for _, c := range completed {
			sb.WriteString(c + "\n")
		}
	}
	if len(explored) > 0 {
		sb.WriteString("\n## Areas Already Analyzed\n")
		sb.WriteString("Skip these files during discovery unless git log shows recent changes:\n")
		for _, e := range explored {
			sb.WriteString(e + "\n")
		}
	}
	sb.WriteString("\nNote: Full history in progress.md. Append new learnings/status to progress.md.\n")
	return atomicWrite(path, []byte(sb.String()))
}

func writeEmptyContext(path string) error {
	return atomicWrite(path, []byte("# Progress Context (auto-generated — do not edit)\n"+
		"Summary: 0 iterations completed, 0 tasks done\n\nNo prior progress recorded.\n\n"+
		"Note: Full history in progress.md. Append new learnings/status to progress.md.\n"))
}

func extractEntries(lines []string, marker string, max int) []string {
	var matches []string
	for _, line := range lines {
		if strings.Contains(line, marker) {
			matches = append(matches, line)
		}
	}
	if max > 0 && len(matches) > max {
		matches = matches[len(matches)-max:]
	}
	return matches
}

func extractExploredAreas(lines []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range lines {
		idx := strings.Index(line, "EXPLORED:")
		if idx < 0 {
			continue
		}
		path := strings.TrimSpace(line[idx+len("EXPLORED:"):])
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, "- "+path)
	}
	return out
}

func countProgressStats(lines []string) (iterations int, completions int) {
	seen := map[string]bool{}
	for _, line := range lines {
		if strings.Contains(line, "COMPLETED:") {
			completions++
		}
		if idx := strings.Index(line, "[iteration:"); idx >= 0 {
			end := strings.Index(line[idx:], "]")
			if end > 0 {
				tag := line[idx : idx+end+1]
				seen[tag] = true
			}
		}
	}
	return len(seen), completions
}

func readAllLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	return lines, s.Err()
}
