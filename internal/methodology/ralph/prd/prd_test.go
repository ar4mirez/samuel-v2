package prd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.toon")

	p := NewAutoPRD("demo", "round-trip test")
	if err := p.AddTask(AutoTask{ID: "1", Title: "Bootstrap", Priority: PriorityHigh, Complexity: ComplexityMedium}); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := p.AddTask(AutoTask{
		ID:            "2",
		Title:         "Refactor",
		Status:        StatusInProgress,
		DependsOn:     []string{"1"},
		FilesToModify: []string{"a.go", "b.go"},
	}); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := p.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(got.Tasks) != 2 {
		t.Fatalf("task count: want 2 got %d", len(got.Tasks))
	}
	if got.Tasks[1].FilesToModify[0] != "a.go" {
		t.Fatalf("files_to_modify not preserved: %v", got.Tasks[1].FilesToModify)
	}
	if got.Tasks[1].DependsOn[0] != "1" {
		t.Fatalf("depends_on not preserved: %v", got.Tasks[1].DependsOn)
	}
}

func TestNextAvailableID(t *testing.T) {
	p := NewAutoPRD("demo", "")
	if got := p.NextAvailableID(); got != "1" {
		t.Fatalf("empty PRD: want 1 got %s", got)
	}
	_ = p.AddTask(AutoTask{ID: "1", Title: "a"})
	_ = p.AddTask(AutoTask{ID: "2.0", Title: "b"})
	_ = p.AddTask(AutoTask{ID: "2.1", Title: "c"})
	if got := p.NextAvailableID(); got != "3" {
		t.Fatalf("after 1, 2.0, 2.1: want 3 got %s", got)
	}
}

func TestGetNextTask_HonorsDependencies(t *testing.T) {
	p := NewAutoPRD("demo", "")
	_ = p.AddTask(AutoTask{ID: "1", Title: "root", Priority: PriorityMedium})
	_ = p.AddTask(AutoTask{ID: "2", Title: "child", Priority: PriorityHigh, DependsOn: []string{"1"}})

	t1 := p.GetNextTask()
	if t1 == nil || t1.ID != "1" {
		t.Fatalf("dependency blocked high-priority child; got %v", t1)
	}
	_ = p.CompleteTask("1", "sha", 1)
	t2 := p.GetNextTask()
	if t2 == nil || t2.ID != "2" {
		t.Fatalf("after completing 1, expected 2 to be available; got %v", t2)
	}
}

func TestValidate_Catches_Errors(t *testing.T) {
	p := &AutoPRD{Version: ""}
	p.Tasks = []AutoTask{
		{ID: "", Title: "missing id"},
		{ID: "1", Title: "", Status: "weird"},
		{ID: "1", Title: "duplicate", Status: StatusPending},
		{ID: "2", Title: "bad dep", Status: StatusPending, DependsOn: []string{"99"}},
	}
	errs := Validate(p)
	if len(errs) < 4 {
		t.Fatalf("expected multiple errors; got %v", errs)
	}
}

func TestSave_TOONHeaderPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.toon")
	p := NewAutoPRD("demo", "")
	if err := p.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Reuse load; if the version header was missing decode would fail.
	if _, _, err := Load(path); err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
}

func TestStrJoinSplit(t *testing.T) {
	s := []string{"a", "b", "c"}
	out := strSplit(strJoin(s))
	if strings.Join(out, ",") != "a,b,c" {
		t.Fatalf("round-trip mismatch: %v", out)
	}
	if strJoin(nil) != "" {
		t.Fatalf("empty slice should encode to empty string")
	}
	if got := strSplit(""); got != nil {
		t.Fatalf("empty string should decode to nil; got %v", got)
	}
}
