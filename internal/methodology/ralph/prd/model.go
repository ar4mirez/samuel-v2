// Package prd is the v2 port of samuel_v1/internal/core/auto.go +
// auto_tasks.go — the data model the Ralph methodology stores in
// .samuel/run/prd.toon.
//
// The on-disk encoding is TOON (see encoding/toon and prd.go), but the
// in-memory shape stays close enough to v1 that older test fixtures
// and prompts keep working: `tasks` is still an ordered slice of
// AutoTask, `progress` is still a flat summary struct, etc.
//
// CLI mutation guarantee: prd.toon is only ever written through the
// methods on AutoPRD (Save). Agents call `samuel run done|skip|reset`
// which go through the same path — they never edit the file directly.
package prd

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the value stamped into the `version` field on every
// prd.toon write. Bumped only on breaking schema changes.
const SchemaVersion = "2.0"

// Runtime directory + canonical filenames used by Ralph.
const (
	RunDir              = ".samuel/run"
	PRDFile             = "prd.toon"
	ProgressFile        = "progress.md"
	ProgressContextFile = "progress-context.md"
	TaskContextFile     = "task-context.toon"
	SnapshotFile        = "project-snapshot.toon"
	PromptFile          = "prompt.md"
	DiscoveryPromptFile = "discovery-prompt.md"
)

// Task status constants — same lifecycle as v1, kept verbatim so v1
// PRDs converted via `samuel run convert` keep their existing status
// strings.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusSkipped    = "skipped"
	StatusBlocked    = "blocked"
)

// Priority + complexity vocabularies match v1.
const (
	PriorityCritical = "critical"
	PriorityHigh     = "high"
	PriorityMedium   = "medium"
	PriorityLow      = "low"

	ComplexitySimple  = "simple"
	ComplexityMedium  = "medium"
	ComplexityComplex = "complex"
)

// Source values — where a task came from. "pilot-discovery" is the
// signal that a discovery iteration generated it.
const (
	SourceManual    = "manual"
	SourcePRD       = "prd"
	SourceDiscovery = "pilot-discovery"
)

// Loop status — overall progress reporter on AutoProgress.
const (
	LoopStatusNotStarted = "not_started"
	LoopStatusRunning    = "running"
	LoopStatusPaused     = "paused"
	LoopStatusCompleted  = "completed"
	LoopStatusFailed     = "failed"
)

// Default pilot constants — identical to v1.
const (
	DefaultPilotIterations      = 30
	DefaultDiscoverInterval     = 5
	DefaultMaxDiscoveryTasks    = 10
	DefaultPauseSecs            = 2
	DefaultMaxConsecFails       = 3
	MinPendingTasksForDiscovery = 2
	MaxEmptyDiscoveries         = 2
)

// AutoPRD is the parsed prd.toon.
type AutoPRD struct {
	Version  string       `toon:"version"`
	Project  AutoProject  `toon:"project"`
	Config   AutoConfig   `toon:"config"`
	Tasks    []AutoTask   `toon:"tasks"`
	Progress AutoProgress `toon:"progress"`
}

// AutoProject is project metadata.
type AutoProject struct {
	Name        string `toon:"name"`
	Description string `toon:"description"`
	SourcePRD   string `toon:"source_prd,omitempty"`
	CreatedAt   string `toon:"created_at"`
	UpdatedAt   string `toon:"updated_at"`
}

// AutoConfig is loop configuration. The encoding/sandbox/methodology
// fields are written verbatim to prd.toon so an agent reading the file
// sees the same configuration the loop will use.
type AutoConfig struct {
	MaxIterations        int          `toon:"max_iterations"`
	QualityChecks        []string     `toon:"quality_checks"`
	AITool               string       `toon:"ai_tool"`
	PromptFile           string       `toon:"ai_prompt_file"`
	Sandbox              string       `toon:"sandbox"`
	SandboxImage         string       `toon:"sandbox_image,omitempty"`
	SandboxTemplate      string       `toon:"sandbox_template,omitempty"`
	PilotMode            bool         `toon:"pilot_mode,omitempty"`
	PilotConfig          *PilotConfig `toon:"pilot_config,omitempty"`
	DiscoveryPromptFile  string       `toon:"discovery_prompt_file,omitempty"`
	ProgressMaxLearnings int          `toon:"progress_max_learnings,omitempty"`
	ProgressMaxCompleted int          `toon:"progress_max_completed,omitempty"`
	ProgressMaxLines     int          `toon:"progress_max_lines,omitempty"`
	Methodology          string       `toon:"methodology,omitempty"`
}

// PilotConfig holds pilot-mode-specific configuration.
type PilotConfig struct {
	DiscoverInterval  int    `toon:"discover_interval"`
	MaxDiscoveryTasks int    `toon:"max_discovery_tasks"`
	Focus             string `toon:"focus,omitempty"`
}

// AutoTask is one item in the Ralph queue.
type AutoTask struct {
	ID            string   `toon:"id"`
	Title         string   `toon:"title"`
	Description   string   `toon:"description,omitempty"`
	Status        string   `toon:"status"`
	Priority      string   `toon:"priority,omitempty"`
	Complexity    string   `toon:"complexity,omitempty"`
	ParentID      string   `toon:"parent_id,omitempty"`
	DependsOn     []string `toon:"depends_on,omitempty"`
	FilesToCreate []string `toon:"files_to_create,omitempty"`
	FilesToModify []string `toon:"files_to_modify,omitempty"`
	Guardrails    []string `toon:"guardrails,omitempty"`
	CompletedAt   string   `toon:"completed_at,omitempty"`
	CommitSHA     string   `toon:"commit_sha,omitempty"`
	Iteration     int      `toon:"iteration,omitempty"`
	Source        string   `toon:"source,omitempty"`
}

// AutoProgress is the rollup summary written under [progress] in
// prd.toon. RecalculateProgress maintains it before each Save.
type AutoProgress struct {
	TotalTasks          int    `toon:"total_tasks"`
	CompletedTasks      int    `toon:"completed_tasks"`
	CurrentIteration    int    `toon:"current_iteration"`
	TotalIterationsRun  int    `toon:"total_iterations_run"`
	LastIterationAt     string `toon:"last_iteration_at,omitempty"`
	Status              string `toon:"status"`
	DiscoveryIterations int    `toon:"discovery_iterations,omitempty"`
	ImplIterations      int    `toon:"impl_iterations,omitempty"`
}

// NewAutoPRD constructs a fresh AutoPRD with the v2 defaults that
// match RFD 0006: ralph methodology, claude adapter, sandbox=oci.
func NewAutoPRD(name, description string) *AutoPRD {
	now := time.Now().UTC().Format(time.RFC3339)
	return &AutoPRD{
		Version: SchemaVersion,
		Project: AutoProject{
			Name:        name,
			Description: description,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Config: AutoConfig{
			MaxIterations: 50,
			QualityChecks: []string{"go test ./...", "go vet ./...", "go build ./..."},
			AITool:        "claude",
			PromptFile:    filepath.Join(RunDir, PromptFile),
			Sandbox:       "none",
			Methodology:   "ralph",
		},
		Tasks: []AutoTask{},
		Progress: AutoProgress{
			Status: LoopStatusNotStarted,
		},
	}
}

// PRDPath is the canonical prd.toon location for projectDir.
func PRDPath(projectDir string) string { return filepath.Join(projectDir, RunDir, PRDFile) }

// RunPath is the canonical .samuel/run directory for projectDir.
func RunPath(projectDir string) string { return filepath.Join(projectDir, RunDir) }

// NextAvailableID returns the next free top-level integer task ID
// (used by `samuel run enqueue`). Matches v1 semantics — top-level IDs
// are integers; nested IDs like "1.1" do not move the counter.
func (p *AutoPRD) NextAvailableID() string {
	maxID := 0
	for _, t := range p.Tasks {
		head := t.ID
		if idx := strings.Index(head, "."); idx >= 0 {
			head = head[:idx]
		}
		n, err := strconv.Atoi(head)
		if err != nil {
			continue
		}
		if n > maxID {
			maxID = n
		}
	}
	return strconv.Itoa(maxID + 1)
}

// GetNextTask returns the highest-priority pending task whose
// dependencies are all completed (or skipped). Returns nil when the
// queue is empty or every pending task is blocked on something open.
func (p *AutoPRD) GetNextTask() *AutoTask {
	available := p.availableTasks()
	if len(available) == 0 {
		return nil
	}
	sort.Slice(available, func(i, j int) bool {
		pi := priorityRank(available[i].Priority)
		pj := priorityRank(available[j].Priority)
		if pi != pj {
			return pi < pj
		}
		return available[i].ID < available[j].ID
	})
	return available[0]
}

func (p *AutoPRD) availableTasks() []*AutoTask {
	done := map[string]bool{}
	for i := range p.Tasks {
		if p.Tasks[i].Status == StatusCompleted || p.Tasks[i].Status == StatusSkipped {
			done[p.Tasks[i].ID] = true
		}
	}
	var available []*AutoTask
	for i := range p.Tasks {
		if p.Tasks[i].Status != StatusPending {
			continue
		}
		ok := true
		for _, dep := range p.Tasks[i].DependsOn {
			if !done[dep] {
				ok = false
				break
			}
		}
		if ok {
			available = append(available, &p.Tasks[i])
		}
	}
	return available
}

func priorityRank(p string) int {
	switch p {
	case PriorityCritical:
		return 0
	case PriorityHigh:
		return 1
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 3
	}
	return 2
}

// findTask returns a pointer to the task with the given ID.
func (p *AutoPRD) findTask(id string) *AutoTask {
	for i := range p.Tasks {
		if p.Tasks[i].ID == id {
			return &p.Tasks[i]
		}
	}
	return nil
}

// CompleteTask marks a task as completed with commit info.
func (p *AutoPRD) CompleteTask(id, commitSHA string, iteration int) error {
	t := p.findTask(id)
	if t == nil {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Status = StatusCompleted
	t.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	t.CommitSHA = commitSHA
	t.Iteration = iteration
	return nil
}

// SkipTask marks a task as skipped.
func (p *AutoPRD) SkipTask(id string) error {
	t := p.findTask(id)
	if t == nil {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Status = StatusSkipped
	return nil
}

// ResetTask clears completion fields and returns the task to pending.
func (p *AutoPRD) ResetTask(id string) error {
	t := p.findTask(id)
	if t == nil {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Status = StatusPending
	t.CompletedAt = ""
	t.CommitSHA = ""
	t.Iteration = 0
	return nil
}

// AddTask appends a new task to the queue. ID is required and must be
// unique; Status defaults to pending.
func (p *AutoPRD) AddTask(t AutoTask) error {
	if t.ID == "" {
		return fmt.Errorf("task ID is required")
	}
	if p.findTask(t.ID) != nil {
		return fmt.Errorf("task with ID %s already exists", t.ID)
	}
	if t.Status == "" {
		t.Status = StatusPending
	}
	p.Tasks = append(p.Tasks, t)
	return nil
}

// CountPendingTasks reports how many tasks are still pending — used by
// pilot mode's ShouldRunDiscovery.
func (p *AutoPRD) CountPendingTasks() int {
	c := 0
	for _, t := range p.Tasks {
		if t.Status == StatusPending {
			c++
		}
	}
	return c
}

// RecalculateProgress updates AutoProgress fields from the task list.
// Called automatically by Save.
func (p *AutoPRD) RecalculateProgress() {
	total := 0
	completed := 0
	for _, t := range p.Tasks {
		total++
		if t.Status == StatusCompleted {
			completed++
		}
	}
	p.Progress.TotalTasks = total
	p.Progress.CompletedTasks = completed
	if total > 0 && completed == total {
		p.Progress.Status = LoopStatusCompleted
	}
}

// Validate runs structural checks and returns every issue found.
func Validate(p *AutoPRD) []string {
	var errs []string
	if p.Version == "" {
		errs = append(errs, "version is required")
	}
	if p.Project.Name == "" {
		errs = append(errs, "project.name is required")
	}
	ids := map[string]bool{}
	for _, t := range p.Tasks {
		if t.ID == "" {
			errs = append(errs, "task missing ID")
			continue
		}
		if ids[t.ID] {
			errs = append(errs, fmt.Sprintf("duplicate task ID: %s", t.ID))
		}
		ids[t.ID] = true
		if t.Title == "" {
			errs = append(errs, fmt.Sprintf("task %s missing title", t.ID))
		}
		if !validStatus(t.Status) {
			errs = append(errs, fmt.Sprintf("task %s has invalid status: %s", t.ID, t.Status))
		}
	}
	for _, t := range p.Tasks {
		for _, dep := range t.DependsOn {
			if !ids[dep] {
				errs = append(errs, fmt.Sprintf("task %s depends on unknown task: %s", t.ID, dep))
			}
		}
	}
	return errs
}

func validStatus(s string) bool {
	return slices.Contains([]string{StatusPending, StatusInProgress, StatusCompleted, StatusSkipped, StatusBlocked}, s)
}
