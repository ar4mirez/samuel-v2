package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelpkg/samuel/internal/agents"
	"github.com/samuelpkg/samuel/internal/methodology/hooks"
	"github.com/samuelpkg/samuel/internal/methodology/ralph"
	"github.com/samuelpkg/samuel/internal/methodology/ralph/prd"
)

func setupRunProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(dir, prd.RunDir), 0o755)
	p := prd.NewAutoPRD("demo", "")
	_ = p.AddTask(prd.AutoTask{ID: "1", Title: "first", Status: prd.StatusPending})
	_ = p.AddTask(prd.AutoTask{ID: "2", Title: "second", Status: prd.StatusPending})
	if err := p.Save(prd.PRDPath(dir)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunMutate_Done_PersistsCompletion(t *testing.T) {
	dir := setupRunProject(t)
	cmd := runDoneCmd
	_ = cmd.ParseFlags([]string{"--commit-sha", "abc123def", "--iteration", "5"})
	if err := runRunTaskDone(cmd, []string{"1"}); err != nil {
		t.Fatalf("runRunTaskDone: %v", err)
	}
	got, _, _ := prd.Load(prd.PRDPath(dir))
	if got.Tasks[0].Status != prd.StatusCompleted {
		t.Fatalf("task 1 not marked completed; got %s", got.Tasks[0].Status)
	}
	if got.Tasks[0].CommitSHA != "abc123def" {
		t.Fatalf("commit sha not persisted; got %q", got.Tasks[0].CommitSHA)
	}
	if got.Tasks[0].Iteration != 5 {
		t.Fatalf("iteration not persisted; got %d", got.Tasks[0].Iteration)
	}
}

func TestRunMutate_Skip_WithReason(t *testing.T) {
	_ = setupRunProject(t)
	cmd := runSkipCmd
	cmd.ResetFlags()
	cmd.Flags().String("reason", "", "")
	_ = cmd.ParseFlags([]string{"--reason", "covered by 2"})
	if err := runRunTaskSkip(cmd, []string{"1"}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	got, _, _ := prd.Load(prd.PRDPath("."))
	if got.Tasks[0].Status != prd.StatusSkipped {
		t.Fatalf("task 1 not skipped; got %s", got.Tasks[0].Status)
	}
}

func TestRunMutate_Enqueue_AutoAssignsID(t *testing.T) {
	_ = setupRunProject(t)
	cmd := runEnqueueCmd
	cmd.ResetFlags()
	cmd.Flags().String("priority", prd.PriorityMedium, "")
	cmd.Flags().String("complexity", prd.ComplexityMedium, "")
	cmd.Flags().String("source", prd.SourceManual, "")
	_ = cmd.ParseFlags(nil)
	if err := runRunTaskEnqueue(cmd, []string{"Hello"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	got, _, _ := prd.Load(prd.PRDPath("."))
	if got.Tasks[len(got.Tasks)-1].ID != "3" {
		t.Fatalf("expected next id 3; got %s", got.Tasks[len(got.Tasks)-1].ID)
	}
}

func TestE2ELoop_AgentEmitsCLIDone_TaskCompletes(t *testing.T) {
	dir := setupRunProject(t)
	// Simulate the agent by directly mutating prd.toon between
	// iterations through CompleteTask — this models a CLI subcommand
	// invocation. The loop's reload pattern picks it up.
	registry := hooks.NewRegistry()
	ralph.RegisterDefaults(registry)
	adapter, _ := agents.Get("claude")
	cfg := ralph.LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 2,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        &fakeAgent{},
		OnIterEnd: func(iter int, err error) {
			// Mid-loop, mark task 1 done as if the agent had run
			// `samuel run done 1`.
			if iter != 1 {
				return
			}
			p, _, _ := prd.Load(prd.PRDPath(dir))
			_ = p.CompleteTask("1", "deadbeef", 1)
			_ = p.Save(prd.PRDPath(dir))
		},
	}
	if err := ralph.RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("loop: %v", err)
	}
	got, _, _ := prd.Load(prd.PRDPath(dir))
	if got.Tasks[0].Status != prd.StatusCompleted {
		t.Fatalf("task 1 should be completed; got %s", got.Tasks[0].Status)
	}
}

func TestAgnostic_NoClaudePathsWritten(t *testing.T) {
	dir := setupRunProject(t)
	registry := hooks.NewRegistry()
	ralph.RegisterDefaults(registry)
	adapter, _ := agents.Get("claude")
	cfg := ralph.LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 1,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        &fakeAgent{},
	}
	if err := ralph.RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("loop: %v", err)
	}
	walked := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		walked++
		if strings.Contains(path, ".claude/") {
			t.Fatalf("agnostic invariant violated: %s under .claude/", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if walked == 0 {
		t.Fatal("expected at least one file to be present")
	}
}

func TestTOON_MalformedRow_LoopContinues(t *testing.T) {
	dir := setupRunProject(t)
	// Corrupt a row in prd.toon by appending a garbage row that doesn't
	// have the right column count. The TOON decoder records a warning
	// and skips the row; the loop should reload + carry on.
	prdPath := prd.PRDPath(dir)
	body, err := os.ReadFile(prdPath)
	if err != nil {
		t.Fatal(err)
	}
	// Insert a malformed row inside the tasks table by bumping the
	// declared row count by 1 and appending a row with the wrong cell
	// count. The TOON decoder will record a warning and skip that row,
	// keeping the rest of the table intact.
	src := string(body)
	// `tasks[2]{...}` → `tasks[3]{...}`
	src = strings.Replace(src, "tasks[2]", "tasks[3]", 1)
	// Append a malformed third row after the existing two.
	lines := strings.Split(src, "\n")
	var rebuilt []string
	inserted := 0
	for _, l := range lines {
		rebuilt = append(rebuilt, l)
		if strings.HasPrefix(l, "  ") && strings.Count(l, ",") > 5 {
			inserted++
			if inserted == 2 {
				rebuilt = append(rebuilt, "  not,enough,cells")
			}
		}
	}
	if inserted < 2 {
		t.Skip("could not locate enough task rows to corrupt")
	}
	corrupted := strings.Join(rebuilt, "\n")
	if err := os.WriteFile(prdPath, []byte(corrupted), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := hooks.NewRegistry()
	ralph.RegisterDefaults(registry)
	adapter, _ := agents.Get("claude")
	cfg := ralph.LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prdPath,
		MaxIterations: 1,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        &fakeAgent{},
	}
	if err := ralph.RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("loop must survive corrupted row: %v", err)
	}
}

type fakeAgent struct{}

func (f *fakeAgent) Run(_ context.Context, _ string, _ []string, _ agents.CommandOptions) (agents.Result, error) {
	return agents.Result{Stdout: "fake"}, nil
}
