package prd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inlineTasksPRD = `# PRD 0001 — Smoke Test

## Goals

1. Validate the pipeline.

## Tasks

### 1.1 Render the dry-run prompt

The loop should produce a context bundle and feed it to the adapter.

**Acceptance**: prompt rendered, exit 0.

### 1.2 Honor the iteration cap

A single iteration should suffice in dry-run.

**Acceptance**: ` + "`iterations_run == 1`" + ` in the run record.

## Out of Scope

Anything beyond dry-run wiring.
`

func TestParseTasksFromPRDBody_HappyPath(t *testing.T) {
	tasks := ParseTasksFromPRDBody(inlineTasksPRD)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d: %+v", len(tasks), tasks)
	}
	if tasks[0].ID != "1.1" || tasks[0].Title != "Render the dry-run prompt" {
		t.Errorf("task 0 wrong: %+v", tasks[0])
	}
	if tasks[1].ID != "1.2" || tasks[1].Title != "Honor the iteration cap" {
		t.Errorf("task 1 wrong: %+v", tasks[1])
	}
	if tasks[0].ParentID != "1" || tasks[1].ParentID != "1" {
		t.Errorf("expected ParentID=1 on both; got %q / %q", tasks[0].ParentID, tasks[1].ParentID)
	}
	if !strings.Contains(tasks[0].Description, "context bundle") {
		t.Errorf("task 0 description should carry section body; got %q", tasks[0].Description)
	}
	if !strings.Contains(tasks[0].Description, "Acceptance") {
		t.Errorf("task 0 description should include acceptance line; got %q", tasks[0].Description)
	}
	// The "## Out of Scope" heading must close the tasks-section so its
	// body doesn't bleed into task 2's description.
	if strings.Contains(tasks[1].Description, "Out of Scope") {
		t.Errorf("description leaked past next H2: %q", tasks[1].Description)
	}
}

func TestParseTasksFromPRDBody_NoTasksSectionReturnsNil(t *testing.T) {
	prd := "# Just a PRD\n\n## Goals\n\nA description.\n\n### 1.1 Not a task\n\nBody."
	if got := ParseTasksFromPRDBody(prd); len(got) != 0 {
		t.Errorf("expected 0 tasks when no tasks section; got %+v", got)
	}
}

func TestParseTasksFromPRDBody_SynonymSectionHeadings(t *testing.T) {
	cases := []string{
		"## Tasks",
		"## tasks",
		"## TASKS",
		"## Implementation",
		"## Implementation Plan",
		"## Steps",
		"## Work Items",
	}
	for _, heading := range cases {
		prd := "# x\n\n" + heading + "\n\n### 1 only-task\n\nbody\n"
		got := ParseTasksFromPRDBody(prd)
		if len(got) != 1 {
			t.Errorf("heading %q: expected 1 task; got %d", heading, len(got))
		}
	}
}

func TestParseTasksFromPRDBody_NestedTaskIDsInferParent(t *testing.T) {
	prd := "## Tasks\n\n### 1 Top\n\nbody\n\n### 1.1 Child\n\nbody\n\n### 1.2.3 Grandchild\n\nbody\n"
	got := ParseTasksFromPRDBody(prd)
	if len(got) != 3 {
		t.Fatalf("expected 3 tasks; got %+v", got)
	}
	want := map[string]string{
		"1":     "",
		"1.1":   "1",
		"1.2.3": "1.2",
	}
	for _, task := range got {
		if got := task.ParentID; got != want[task.ID] {
			t.Errorf("task %s: ParentID=%q want %q", task.ID, got, want[task.ID])
		}
	}
}

func TestConvertMarkdownToPRD_FallsBackToInlineTasksWhenNoCompanionFile(t *testing.T) {
	// Regression for issue #4: pre-rc.12, a PRD with inline `### N.M`
	// headings produced zero tasks because the converter only looked
	// at a companion `tasks-<base>.md` file. The smoke test from the
	// rc.6 manual test reproduced this. Now the inline-heading
	// parser kicks in as fallback.
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(prdPath, []byte(inlineTasksPRD), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := ConvertMarkdownToPRD(prdPath, "")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(p.Tasks) != 2 {
		t.Errorf("expected 2 inline tasks; got %d: %+v", len(p.Tasks), p.Tasks)
	}
}

func TestConvertMarkdownToPRD_CompanionFilePreferredWhenPresent(t *testing.T) {
	// Backward compatibility: if the user has both a companion
	// checklist AND inline headings, the companion wins. This
	// preserves v1's contract for generate-tasks fixtures.
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.md")
	tasksPath := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(prdPath, []byte(inlineTasksPRD), 0o644); err != nil {
		t.Fatal(err)
	}
	checklist := "- [ ] 1.0 v1-style task [~1,000 tokens - Simple]\n"
	if err := os.WriteFile(tasksPath, []byte(checklist), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := ConvertMarkdownToPRD(prdPath, tasksPath)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(p.Tasks) != 1 {
		t.Fatalf("expected 1 task from companion checklist; got %d", len(p.Tasks))
	}
	if p.Tasks[0].Title != "v1-style task" {
		t.Errorf("companion checklist not used; got %+v", p.Tasks[0])
	}
}

func TestConvertMarkdownToPRD_EmptyCompanionFileFallsThroughToInline(t *testing.T) {
	// Edge case: user named a companion file but it has no checklist
	// lines. ParseTaskMarkdown errors out, but the converter should
	// not propagate that as a fatal — it should fall through to the
	// inline-heading parser.
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.md")
	tasksPath := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(prdPath, []byte(inlineTasksPRD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tasksPath, []byte("# no checklist here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := ConvertMarkdownToPRD(prdPath, tasksPath)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(p.Tasks) != 2 {
		t.Errorf("expected fallback to inline tasks; got %d", len(p.Tasks))
	}
}
