// Package context generates the pre-computed context files Ralph
// hands to the agent at the start of every iteration. These are the
// "token-discipline innovations" v1 invented: they let the agent skip
// re-scanning the project on every fresh-context iteration.
//
// Output files (all written to .samuel/run/):
//
//   - project-snapshot.toon  — file inventory, test gaps, large files, TODOs, git log (tabular)
//   - progress-context.md    — markdown summary of prior iterations
//   - task-context.toon      — current-task brief (impl) or summary (discovery)
//
// Markdown is preserved for progress-context.md (humans skim it) while
// the structural files use TOON for token efficiency.
package context

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ar4mirez/samuel/internal/encoding/toon"
	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
)

const (
	snapshotMaxFiles    = 200
	snapshotMaxGitLog   = 10
	snapshotMaxTODOList = 30
	snapshotMaxLarge    = 15
)

// FileEntry is one row in the snapshot's `files` table.
type FileEntry struct {
	Path string
	Size int64
}

// GenerateProjectSnapshot writes project-snapshot.toon at
// projectDir/.samuel/run/project-snapshot.toon.
func GenerateProjectSnapshot(projectDir string) error {
	path := filepath.Join(prd.RunPath(projectDir), prd.SnapshotFile)
	files, err := collectProjectFiles(projectDir)
	if err != nil {
		return fmt.Errorf("collect project files: %w", err)
	}
	testGaps := findTestGaps(files)
	largeFiles := findLargeFiles(files)
	todoCounts := countTODOMarkers(projectDir, files)
	gitLog := recentGitLog(projectDir)

	root := toon.NewObject()
	root.Set("files", encodeFileTable(files))
	root.Set("test_gaps", toAnyStrings(testGaps))
	root.Set("large_files", encodeFileTable(largeFiles))
	root.Set("todo_counts", encodeTODOTable(todoCounts))
	root.Set("git_log", toAnyStrings(gitLog))

	body, err := toon.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicWrite(path, body)
}

func encodeFileTable(files []FileEntry) *toon.TableArray {
	t := toon.NewTableArray("path", "size")
	for _, f := range files {
		t.AddRow(map[string]toon.Value{"path": f.Path, "size": int64(f.Size)})
	}
	return t
}

func encodeTODOTable(counts map[string]int) *toon.TableArray {
	t := toon.NewTableArray("path", "count")
	type entry struct {
		path  string
		count int
	}
	var entries []entry
	for p, c := range counts {
		entries = append(entries, entry{p, c})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].count > entries[j].count })
	limit := snapshotMaxTODOList
	if len(entries) < limit {
		limit = len(entries)
	}
	for _, e := range entries[:limit] {
		t.AddRow(map[string]toon.Value{"path": e.path, "count": int64(e.count)})
	}
	return t
}

func collectProjectFiles(projectDir string) ([]FileEntry, error) {
	var files []FileEntry
	err := filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if shouldSkipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSourceFile(name) {
			return nil
		}
		rel, relErr := filepath.Rel(projectDir, path)
		if relErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		files = append(files, FileEntry{Path: rel, Size: info.Size()})
		if len(files) >= snapshotMaxFiles {
			return filepath.SkipAll
		}
		return nil
	})
	return files, err
}

func shouldSkipDir(name string) bool {
	skip := []string{".git", ".samuel", ".claude", "vendor", "node_modules", "bin", "site", "docs", "template"}
	for _, s := range skip {
		if name == s {
			return true
		}
	}
	return strings.HasPrefix(name, ".")
}

func isSourceFile(name string) bool {
	exts := []string{".go", ".py", ".js", ".ts", ".rs", ".java", ".rb"}
	for _, ext := range exts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func findTestGaps(files []FileEntry) []string {
	testFiles := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f.Path, "_test.go") {
			testFiles[f.Path] = true
		}
	}
	var gaps []string
	for _, f := range files {
		if strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		testPath := strings.TrimSuffix(f.Path, ".go") + "_test.go"
		if !testFiles[testPath] {
			gaps = append(gaps, f.Path)
		}
	}
	return gaps
}

func findLargeFiles(files []FileEntry) []FileEntry {
	sorted := make([]FileEntry, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Size > sorted[j].Size })
	limit := snapshotMaxLarge
	if len(sorted) < limit {
		limit = len(sorted)
	}
	return sorted[:limit]
}

func countTODOMarkers(projectDir string, files []FileEntry) map[string]int {
	counts := map[string]int{}
	markers := []string{"TODO", "FIXME", "HACK"}
	for _, f := range files {
		if strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		c := countMarkersInFile(filepath.Join(projectDir, f.Path), markers)
		if c > 0 {
			counts[f.Path] = c
		}
	}
	return counts
}

func countMarkersInFile(path string, markers []string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		for _, m := range markers {
			if strings.Contains(line, m) {
				count++
				break
			}
		}
	}
	return count
}

func recentGitLog(projectDir string) []string {
	cmd := exec.Command("git", "log", "--oneline", fmt.Sprintf("-n%d", snapshotMaxGitLog))
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func toAnyStrings(s []string) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}

func atomicWrite(path string, body []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
