package prd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ar4mirez/samuel/internal/encoding/toon"
)

// Load reads prd.toon from path and decodes it into an AutoPRD. Per-row
// malformations in the `tasks` table are recovered: the row is skipped
// and the decoder Warning surfaces upstream.
func Load(path string) (*AutoPRD, []toon.Warning, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read prd.toon: %w", err)
	}
	root, warnings, err := toon.Unmarshal(raw)
	if err != nil {
		return nil, warnings, fmt.Errorf("parse prd.toon: %w", err)
	}
	p, err := decodePRD(root)
	if err != nil {
		return nil, warnings, err
	}
	return p, warnings, nil
}

// Save serializes p into TOON and writes prd.toon atomically
// (write-tmp-then-rename). Project.UpdatedAt is refreshed and progress
// is recalculated before encoding.
func (p *AutoPRD) Save(path string) error {
	p.Project.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	p.RecalculateProgress()
	if p.Version == "" {
		p.Version = SchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for prd.toon: %w", err)
	}
	root := encodePRD(p)
	body, err := toon.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal prd.toon: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write tmp prd.toon: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename prd.toon: %w", err)
	}
	return nil
}

func encodePRD(p *AutoPRD) *toon.Object {
	root := toon.NewObject()
	root.Set("version", p.Version)
	root.Set("project", encodeProject(p.Project))
	root.Set("config", encodeConfig(p.Config))
	root.Set("tasks", encodeTaskTable(p.Tasks))
	root.Set("progress", encodeProgress(p.Progress))
	return root
}

func encodeProject(pr AutoProject) *toon.Object {
	o := toon.NewObject()
	o.Set("name", pr.Name)
	o.Set("description", pr.Description)
	if pr.SourcePRD != "" {
		o.Set("source_prd", pr.SourcePRD)
	}
	o.Set("created_at", pr.CreatedAt)
	o.Set("updated_at", pr.UpdatedAt)
	return o
}

func encodeConfig(c AutoConfig) *toon.Object {
	o := toon.NewObject()
	o.Set("max_iterations", int64(c.MaxIterations))
	o.Set("quality_checks", toAnySlice(c.QualityChecks))
	o.Set("ai_tool", c.AITool)
	o.Set("ai_prompt_file", c.PromptFile)
	o.Set("sandbox", c.Sandbox)
	if c.SandboxImage != "" {
		o.Set("sandbox_image", c.SandboxImage)
	}
	if c.SandboxTemplate != "" {
		o.Set("sandbox_template", c.SandboxTemplate)
	}
	if c.PilotMode {
		o.Set("pilot_mode", true)
	}
	if c.PilotConfig != nil {
		o.Set("pilot_config", encodePilot(*c.PilotConfig))
	}
	if c.DiscoveryPromptFile != "" {
		o.Set("discovery_prompt_file", c.DiscoveryPromptFile)
	}
	if c.ProgressMaxLearnings > 0 {
		o.Set("progress_max_learnings", int64(c.ProgressMaxLearnings))
	}
	if c.ProgressMaxCompleted > 0 {
		o.Set("progress_max_completed", int64(c.ProgressMaxCompleted))
	}
	if c.ProgressMaxLines > 0 {
		o.Set("progress_max_lines", int64(c.ProgressMaxLines))
	}
	if c.Methodology != "" {
		o.Set("methodology", c.Methodology)
	}
	return o
}

func encodePilot(p PilotConfig) *toon.Object {
	o := toon.NewObject()
	o.Set("discover_interval", int64(p.DiscoverInterval))
	o.Set("max_discovery_tasks", int64(p.MaxDiscoveryTasks))
	if p.Focus != "" {
		o.Set("focus", p.Focus)
	}
	return o
}

func encodeProgress(pr AutoProgress) *toon.Object {
	o := toon.NewObject()
	o.Set("total_tasks", int64(pr.TotalTasks))
	o.Set("completed_tasks", int64(pr.CompletedTasks))
	o.Set("current_iteration", int64(pr.CurrentIteration))
	o.Set("total_iterations_run", int64(pr.TotalIterationsRun))
	if pr.LastIterationAt != "" {
		o.Set("last_iteration_at", pr.LastIterationAt)
	}
	o.Set("status", pr.Status)
	if pr.DiscoveryIterations > 0 {
		o.Set("discovery_iterations", int64(pr.DiscoveryIterations))
	}
	if pr.ImplIterations > 0 {
		o.Set("impl_iterations", int64(pr.ImplIterations))
	}
	return o
}

// taskColumns is the canonical order of columns in the tasks table.
// Keeping it stable lets diffs against prd.toon stay small.
var taskColumns = []string{
	"id", "title", "description", "status", "priority", "complexity",
	"parent_id", "depends_on", "files_to_create", "files_to_modify",
	"guardrails", "completed_at", "commit_sha", "iteration", "source",
}

func encodeTaskTable(tasks []AutoTask) *toon.TableArray {
	t := toon.NewTableArray(taskColumns...)
	for _, task := range tasks {
		t.AddRow(map[string]toon.Value{
			"id":              task.ID,
			"title":           task.Title,
			"description":     task.Description,
			"status":          task.Status,
			"priority":        task.Priority,
			"complexity":      task.Complexity,
			"parent_id":       task.ParentID,
			"depends_on":      strJoin(task.DependsOn),
			"files_to_create": strJoin(task.FilesToCreate),
			"files_to_modify": strJoin(task.FilesToModify),
			"guardrails":      strJoin(task.Guardrails),
			"completed_at":    task.CompletedAt,
			"commit_sha":      task.CommitSHA,
			"iteration":       int64(task.Iteration),
			"source":          task.Source,
		})
	}
	return t
}

// strJoin packs string-slice columns into a single cell using "|" as
// the separator. TOON cells don't natively support nested arrays, so
// we encode them as pipe-delimited strings and split on read.
func strJoin(s []string) string {
	if len(s) == 0 {
		return ""
	}
	out := s[0]
	for _, x := range s[1:] {
		out += "|" + x
	}
	return out
}

func strSplit(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	cur := ""
	for _, c := range s {
		if c == '|' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	out = append(out, cur)
	return out
}

// decodePRD walks the parsed Object tree and rebuilds an AutoPRD.
// Missing optional fields are tolerated; missing required fields
// produce an error.
func decodePRD(root *toon.Object) (*AutoPRD, error) {
	p := &AutoPRD{}
	if v, ok := root.Get("version"); ok {
		p.Version = strOf(v)
	}
	if v, ok := root.Get("project"); ok {
		obj, isObj := v.(*toon.Object)
		if !isObj {
			return nil, fmt.Errorf("prd.toon: project must be an object")
		}
		p.Project = decodeProject(obj)
	}
	if v, ok := root.Get("config"); ok {
		obj, isObj := v.(*toon.Object)
		if !isObj {
			return nil, fmt.Errorf("prd.toon: config must be an object")
		}
		p.Config = decodeConfig(obj)
	}
	if v, ok := root.Get("tasks"); ok {
		if tbl, isTbl := v.(*toon.TableArray); isTbl {
			p.Tasks = decodeTaskTable(tbl)
		}
	}
	if v, ok := root.Get("progress"); ok {
		obj, isObj := v.(*toon.Object)
		if !isObj {
			return nil, fmt.Errorf("prd.toon: progress must be an object")
		}
		p.Progress = decodeProgress(obj)
	}
	return p, nil
}

func decodeProject(o *toon.Object) AutoProject {
	p := AutoProject{}
	get := func(k string) string {
		v, _ := o.Get(k)
		return strOf(v)
	}
	p.Name = get("name")
	p.Description = get("description")
	p.SourcePRD = get("source_prd")
	p.CreatedAt = get("created_at")
	p.UpdatedAt = get("updated_at")
	return p
}

func decodeConfig(o *toon.Object) AutoConfig {
	c := AutoConfig{}
	c.MaxIterations = intOf(o, "max_iterations")
	c.QualityChecks = strArrayOf(o, "quality_checks")
	c.AITool = strField(o, "ai_tool")
	c.PromptFile = strField(o, "ai_prompt_file")
	c.Sandbox = strField(o, "sandbox")
	c.SandboxImage = strField(o, "sandbox_image")
	c.SandboxTemplate = strField(o, "sandbox_template")
	if v, ok := o.Get("pilot_mode"); ok {
		if b, isBool := v.(bool); isBool {
			c.PilotMode = b
		}
	}
	if v, ok := o.Get("pilot_config"); ok {
		if pc, isObj := v.(*toon.Object); isObj {
			p := PilotConfig{
				DiscoverInterval:  intOf(pc, "discover_interval"),
				MaxDiscoveryTasks: intOf(pc, "max_discovery_tasks"),
				Focus:             strField(pc, "focus"),
			}
			c.PilotConfig = &p
		}
	}
	c.DiscoveryPromptFile = strField(o, "discovery_prompt_file")
	c.ProgressMaxLearnings = intOf(o, "progress_max_learnings")
	c.ProgressMaxCompleted = intOf(o, "progress_max_completed")
	c.ProgressMaxLines = intOf(o, "progress_max_lines")
	c.Methodology = strField(o, "methodology")
	return c
}

func decodeTaskTable(t *toon.TableArray) []AutoTask {
	out := make([]AutoTask, 0, len(t.Rows))
	for _, r := range t.Rows {
		if r == nil {
			continue // skip malformed row (warning already recorded on decode)
		}
		task := AutoTask{
			ID:            cellString(r, "id"),
			Title:         cellString(r, "title"),
			Description:   cellString(r, "description"),
			Status:        cellString(r, "status"),
			Priority:      cellString(r, "priority"),
			Complexity:    cellString(r, "complexity"),
			ParentID:      cellString(r, "parent_id"),
			DependsOn:     strSplit(cellString(r, "depends_on")),
			FilesToCreate: strSplit(cellString(r, "files_to_create")),
			FilesToModify: strSplit(cellString(r, "files_to_modify")),
			Guardrails:    strSplit(cellString(r, "guardrails")),
			CompletedAt:   cellString(r, "completed_at"),
			CommitSHA:     cellString(r, "commit_sha"),
			Iteration:     cellInt(r, "iteration"),
			Source:        cellString(r, "source"),
		}
		if task.ID == "" {
			// AI may emit numeric ID — TOON would parse it as int64.
			if v, ok := r["id"]; ok {
				switch n := v.(type) {
				case int64:
					task.ID = fmt.Sprintf("%d", n)
				case float64:
					task.ID = strconv.FormatFloat(n, 'g', -1, 64)
				}
			}
		}
		out = append(out, task)
	}
	return out
}

func decodeProgress(o *toon.Object) AutoProgress {
	return AutoProgress{
		TotalTasks:          intOf(o, "total_tasks"),
		CompletedTasks:      intOf(o, "completed_tasks"),
		CurrentIteration:    intOf(o, "current_iteration"),
		TotalIterationsRun:  intOf(o, "total_iterations_run"),
		LastIterationAt:     strField(o, "last_iteration_at"),
		Status:              strField(o, "status"),
		DiscoveryIterations: intOf(o, "discovery_iterations"),
		ImplIterations:      intOf(o, "impl_iterations"),
	}
}

// --- helpers ---

func strOf(v toon.Value) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func strField(o *toon.Object, k string) string {
	v, ok := o.Get(k)
	if !ok {
		return ""
	}
	return strOf(v)
}

func intOf(o *toon.Object, k string) int {
	v, ok := o.Get(k)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func strArrayOf(o *toon.Object, k string) []string {
	v, ok := o.Get(k)
	if !ok {
		return nil
	}
	arr, isArr := v.([]any)
	if !isArr {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, strOf(e))
	}
	return out
}

func cellString(row map[string]toon.Value, col string) string {
	v, ok := row[col]
	if !ok {
		return ""
	}
	return strOf(v)
}

func cellInt(row map[string]toon.Value, col string) int {
	v, ok := row[col]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func toAnySlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
