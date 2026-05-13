package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
)

func setupRunDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, prd.RunDir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

func TestGenerateProjectSnapshot_WritesFile(t *testing.T) {
	dir := setupRunDir(t)
	// Add a real source file so the snapshot has something to count.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {// TODO\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GenerateProjectSnapshot(dir); err != nil {
		t.Fatalf("GenerateProjectSnapshot: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, prd.RunDir, prd.SnapshotFile))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !strings.HasPrefix(string(body), "# toon v") {
		t.Fatalf("expected TOON header at start; got %q", string(body[:20]))
	}
	if !strings.Contains(string(body), "main.go") {
		t.Fatalf("expected main.go in snapshot; got %s", body)
	}
}

func TestGenerateTaskContext_Implementation(t *testing.T) {
	dir := setupRunDir(t)
	p := prd.NewAutoPRD("demo", "")
	_ = p.AddTask(prd.AutoTask{ID: "1", Title: "Bootstrap", Status: prd.StatusPending, Priority: prd.PriorityHigh})
	if err := GenerateTaskContext(dir, p, false); err != nil {
		t.Fatalf("impl mode: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, prd.RunDir, prd.TaskContextFile))
	if !strings.Contains(string(body), "implementation") {
		t.Fatalf("expected impl mode; got %s", body)
	}
	if !strings.Contains(string(body), "Bootstrap") {
		t.Fatalf("expected task title; got %s", body)
	}
}

func TestGenerateTaskContext_Discovery(t *testing.T) {
	dir := setupRunDir(t)
	p := prd.NewAutoPRD("demo", "")
	_ = p.AddTask(prd.AutoTask{ID: "1", Title: "T1", Status: prd.StatusPending})
	_ = p.AddTask(prd.AutoTask{ID: "2", Title: "T2", Status: prd.StatusCompleted})
	if err := GenerateTaskContext(dir, p, true); err != nil {
		t.Fatalf("discovery: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, prd.RunDir, prd.TaskContextFile))
	if !strings.Contains(string(body), "discovery") {
		t.Fatalf("expected discovery mode; got %s", body)
	}
}

func TestRotateProgress_TriggersAtThreshold(t *testing.T) {
	dir := setupRunDir(t)
	path := filepath.Join(dir, prd.RunDir, prd.ProgressFile)
	var sb strings.Builder
	for i := 0; i < 600; i++ {
		sb.WriteString("line\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RotateProgressIfNeeded(dir, 500); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	// progress.md should be smaller now; an archive should exist.
	info, _ := os.Stat(path)
	if info.Size() >= int64(sb.Len()) {
		t.Fatalf("expected progress.md to shrink; size=%d", info.Size())
	}
	matches, _ := filepath.Glob(filepath.Join(dir, prd.RunDir, "progress-archive-*.md"))
	if len(matches) == 0 {
		t.Fatal("expected an archive file")
	}
}

func TestGenerateProgressContext_WithLearnings(t *testing.T) {
	dir := setupRunDir(t)
	body := "[2026-05-01] [iteration:1] [task:1] LEARNING: be careful\n" +
		"[2026-05-01] [iteration:2] COMPLETED: task 1\n" +
		"[2026-05-01] [discovery] EXPLORED: foo.go\n"
	_ = os.WriteFile(filepath.Join(dir, prd.RunDir, prd.ProgressFile), []byte(body), 0o644)
	if err := GenerateProgressContext(dir, ProgressConfig{}); err != nil {
		t.Fatalf("GenerateProgressContext: %v", err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, prd.RunDir, prd.ProgressContextFile))
	if !strings.Contains(string(out), "be careful") {
		t.Fatalf("expected learnings; got %s", out)
	}
}
