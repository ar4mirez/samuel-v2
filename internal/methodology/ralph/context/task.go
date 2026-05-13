package context

import (
	"fmt"
	"path/filepath"

	"github.com/ar4mirez/samuel/internal/encoding/toon"
	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
)

// GenerateTaskContext writes task-context.toon describing what the
// agent should work on this iteration. The shape depends on mode:
//
//   - implementation: full detail of the next pending task + recent completions
//   - discovery: a summary table of every task + the files already covered
func GenerateTaskContext(projectDir string, p *prd.AutoPRD, isDiscovery bool) error {
	path := filepath.Join(prd.RunPath(projectDir), prd.TaskContextFile)
	root := toon.NewObject()

	if p == nil {
		root.Set("mode", "empty")
		root.Set("message", "No PRD loaded.")
		return marshalAndWrite(path, root)
	}

	if isDiscovery {
		root.Set("mode", "discovery")
		root.Set("summary", encodeDiscoverySummary(p))
		root.Set("tasks", encodeTaskSummaryTable(p))
		root.Set("covered_files", toAnyStrings(coveredFiles(p)))
		return marshalAndWrite(path, root)
	}

	root.Set("mode", "implementation")
	next := p.GetNextTask()
	if next == nil {
		root.Set("message", "No pending tasks available.")
		return marshalAndWrite(path, root)
	}
	root.Set("current_task", encodeTaskDetail(*next))
	root.Set("recent_completions", encodeRecentCompletionsTable(p))
	return marshalAndWrite(path, root)
}

func encodeDiscoverySummary(p *prd.AutoPRD) *toon.Object {
	pending, completed, other := 0, 0, 0
	for _, t := range p.Tasks {
		switch t.Status {
		case prd.StatusPending:
			pending++
		case prd.StatusCompleted:
			completed++
		default:
			other++
		}
	}
	o := toon.NewObject()
	o.Set("total", int64(len(p.Tasks)))
	o.Set("pending", int64(pending))
	o.Set("completed", int64(completed))
	o.Set("other", int64(other))
	return o
}

func encodeTaskSummaryTable(p *prd.AutoPRD) *toon.TableArray {
	t := toon.NewTableArray("id", "status", "priority", "title")
	for _, task := range p.Tasks {
		t.AddRow(map[string]toon.Value{
			"id":       task.ID,
			"status":   task.Status,
			"priority": task.Priority,
			"title":    task.Title,
		})
	}
	return t
}

func coveredFiles(p *prd.AutoPRD) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range p.Tasks {
		if t.Status != prd.StatusPending && t.Status != prd.StatusInProgress {
			continue
		}
		for _, f := range t.FilesToModify {
			if !seen[f] && f != "" {
				seen[f] = true
				out = append(out, f)
			}
		}
		for _, f := range t.FilesToCreate {
			if !seen[f] && f != "" {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

func encodeTaskDetail(t prd.AutoTask) *toon.Object {
	o := toon.NewObject()
	o.Set("id", t.ID)
	o.Set("title", t.Title)
	o.Set("priority", t.Priority)
	o.Set("complexity", t.Complexity)
	if t.Description != "" {
		o.Set("description", t.Description)
	}
	if len(t.FilesToModify) > 0 {
		o.Set("files_to_modify", toAnyStrings(t.FilesToModify))
	}
	if len(t.FilesToCreate) > 0 {
		o.Set("files_to_create", toAnyStrings(t.FilesToCreate))
	}
	if len(t.DependsOn) > 0 {
		o.Set("depends_on", toAnyStrings(t.DependsOn))
	}
	if len(t.Guardrails) > 0 {
		o.Set("guardrails", toAnyStrings(t.Guardrails))
	}
	return o
}

func encodeRecentCompletionsTable(p *prd.AutoPRD) *toon.TableArray {
	const max = 5
	t := toon.NewTableArray("id", "title", "commit_sha")
	count := 0
	for i := len(p.Tasks) - 1; i >= 0 && count < max; i-- {
		task := p.Tasks[i]
		if task.Status != prd.StatusCompleted {
			continue
		}
		t.AddRow(map[string]toon.Value{
			"id":         task.ID,
			"title":      task.Title,
			"commit_sha": task.CommitSHA,
		})
		count++
	}
	return t
}

func marshalAndWrite(path string, root *toon.Object) error {
	body, err := toon.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal task-context: %w", err)
	}
	return atomicWrite(path, body)
}
