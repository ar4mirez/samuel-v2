//go:build e2e

package hermetic

import (
	"testing"
)

// Run-loop block — covers rc.5 (translator default-on for run state),
// rc.8 (empty-queue hint), rc.12 (inline ### N.M task heading parser).

const inlineTaskPRD = `# PRD 0001 — Hermetic Loop Smoke

Used by the e2e suite to validate the inline-task heading parser.

## Goals

1. Validate that the converter recognizes ` + "`### N.M Title`" + ` headings.

## Tasks

### 1.1 First task

Body for task 1.1.

**Acceptance**: parsed by the converter.

### 1.2 Second task

Body for task 1.2.

**Acceptance**: parsed and assigned ParentID "1".
`

func TestRunInit_ParsesInlineTaskHeadings(t *testing.T) {
	p := newProject(t)
	p.writeFile(".samuel/tasks/0001-prd.md", inlineTaskPRD)

	out := p.mustSamuel("run", "init", "--prd", ".samuel/tasks/0001-prd.md")
	assertContains(t, out, "Tasks:   2", "converter must extract both inline tasks")

	tasks := p.mustSamuel("run", "tasks")
	assertContains(t, tasks, "1.1 — First task", "task 1.1 must appear in the list")
	assertContains(t, tasks, "1.2 — Second task", "task 1.2 must appear in the list")
}

func TestRunStart_EmptyQueueHint(t *testing.T) {
	// rc.8 regression: run start used to exit 0 with no output when
	// the queue was empty. Must now print an actionable hint.
	p := newProject(t)
	p.mustSamuel("run", "init") // no --prd, no tasks
	out := p.mustSamuel("run", "start", "--dry-run", "--yes")
	assertContains(t, out, "No pending tasks", "empty-queue start must print the hint")
	assertContains(t, out, "samuel run enqueue", "hint must point at the recovery command")
}

func TestRunStart_DryRunIteratesAgainstFirstTask(t *testing.T) {
	p := newProject(t)
	p.writeFile(".samuel/tasks/0001-prd.md", inlineTaskPRD)
	p.mustSamuel("run", "init", "--prd", ".samuel/tasks/0001-prd.md")

	out := p.mustSamuel("run", "start", "--dry-run", "--iterations", "1", "--yes")
	assertContains(t, out, "Iter 1", "dry-run must surface one iteration line")
	assertContains(t, out, "First task", "first iteration must target task 1.1")
}

func TestRunMutations_DoneSkipResetEnqueue(t *testing.T) {
	p := newProject(t)
	p.writeFile(".samuel/tasks/0001-prd.md", inlineTaskPRD)
	p.mustSamuel("run", "init", "--prd", ".samuel/tasks/0001-prd.md")

	p.mustSamuel("run", "done", "1.1", "--commit-sha", "deadbeef", "--iteration", "1")
	p.mustSamuel("run", "skip", "1.2", "--reason", "covered by 1.1")
	p.mustSamuel("run", "reset", "1.1")
	p.mustSamuel("run", "enqueue", "extra task")

	tasks := p.mustSamuel("run", "tasks")
	assertContains(t, tasks, "[pending     ] 1.1", "1.1 should be back to pending after reset")
	assertContains(t, tasks, "[skipped     ] 1.2", "1.2 should be skipped")
	assertContains(t, tasks, "extra task", "enqueued task must appear")
}
