package ralph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelpkg/samuel/internal/agents"
	"github.com/samuelpkg/samuel/internal/methodology/hooks"
	"github.com/samuelpkg/samuel/internal/methodology/ralph/prd"
)

type fakeRunner struct {
	called int
}

func (f *fakeRunner) Run(_ context.Context, _ string, _ []string, opts agents.CommandOptions) (agents.Result, error) {
	f.called++
	return agents.Result{Stdout: "ok"}, nil
}

func setupRalphProject(t *testing.T) (string, *prd.AutoPRD) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, prd.RunDir), 0o755); err != nil {
		t.Fatal(err)
	}
	p := prd.NewAutoPRD("demo", "loop test")
	_ = p.AddTask(prd.AutoTask{ID: "1", Title: "Solo task", Status: prd.StatusPending})
	if err := p.Save(prd.PRDPath(dir)); err != nil {
		t.Fatal(err)
	}
	return dir, p
}

func TestRunAutoLoop_OneIteration_FiresAllHooks(t *testing.T) {
	dir, p := setupRalphProject(t)
	registry := hooks.NewRegistry()
	RegisterDefaults(registry)
	calls := map[hooks.HookName]int{}
	for _, name := range hooks.AllHookNames() {
		n := name
		registry.RegisterWithOrder(hooks.Func{
			HookName: n,
			Fn: func(_ context.Context, _ hooks.HookInput, _ *hooks.HookOutput) error {
				calls[n]++
				return nil
			},
		}, hooks.SourcePlugin, "tracker", 999)
	}
	runner := &fakeRunner{}
	adapter, _ := agents.Get("claude")
	cfg := LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 1,
		PauseSecs:     0,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        runner,
	}
	if err := RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("RunAutoLoop: %v", err)
	}
	if runner.called == 0 {
		t.Fatal("expected adapter invocation")
	}
	for _, h := range []hooks.HookName{hooks.BeforeLoop, hooks.BeforeIteration, hooks.AfterIteration, hooks.ContextTask, hooks.QualityCheck, hooks.AfterLoop} {
		if calls[h] == 0 {
			t.Fatalf("hook %s never fired", h)
		}
	}
	_ = p
}

func TestRunAutoLoop_EmptyQueue_ExitsCleanly(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, prd.RunDir), 0o755)
	p := prd.NewAutoPRD("demo", "empty queue")
	if err := p.Save(prd.PRDPath(dir)); err != nil {
		t.Fatal(err)
	}
	registry := hooks.NewRegistry()
	RegisterDefaults(registry)
	adapter, _ := agents.Get("claude")
	runner := &fakeRunner{}
	cfg := LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 3,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        runner,
	}
	if err := RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if runner.called != 0 {
		t.Fatal("adapter should not be called on empty queue")
	}
}

func TestRunAutoLoop_DryRun_NeverHitsRealCommand(t *testing.T) {
	dir, _ := setupRalphProject(t)
	registry := hooks.NewRegistry()
	RegisterDefaults(registry)
	adapter, _ := agents.Get("claude")
	runner := &fakeRunner{}
	cfg := LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 1,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        runner,
		DryRun:        true,
	}
	if err := RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("dry-run loop: %v", err)
	}
}

func TestRunAutoLoop_PilotMode_GateRoutesDiscovery(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, prd.RunDir), 0o755)
	p := InitPilotPRD(dir, prd.AutoConfig{
		MaxIterations: 1,
		AITool:        "claude",
		Sandbox:       "none",
	}, NewPilotConfig())
	if err := p.Save(prd.PRDPath(dir)); err != nil {
		t.Fatal(err)
	}
	registry := hooks.NewRegistry()
	RegisterDefaults(registry)
	adapter, _ := agents.Get("claude")
	runner := &fakeRunner{}
	cfg := LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 1,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        runner,
	}
	if err := RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("pilot loop: %v", err)
	}
	got, _, _ := prd.Load(prd.PRDPath(dir))
	if got.Progress.DiscoveryIterations == 0 {
		t.Fatalf("expected pilot mode to log a discovery iteration; progress=%+v", got.Progress)
	}
}

func TestShouldRunDiscovery_EmptyQueueTriggers(t *testing.T) {
	p := prd.NewAutoPRD("demo", "")
	if !ShouldRunDiscovery(p, 5, 1, 10) {
		t.Fatal("empty queue should trigger discovery regardless of interval")
	}
}

func TestShouldRunDiscovery_LowPendingTriggers(t *testing.T) {
	p := prd.NewAutoPRD("demo", "")
	_ = p.AddTask(prd.AutoTask{ID: "1", Title: "x", Status: prd.StatusPending})
	if !ShouldRunDiscovery(p, 3, 1, 10) {
		t.Fatal("pending<2 should preemptively trigger discovery")
	}
}

func TestRender_PromptContainsTaskID(t *testing.T) {
	dir := t.TempDir()
	p := prd.NewAutoPRD("demo", "")
	_ = p.AddTask(prd.AutoTask{ID: "42", Title: "test render", Status: prd.StatusPending})
	out, err := renderPrompt(dir, p, 1, p.GetNextTask(), hooks.IterationTypeImplementation)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "samuel run done 42") {
		t.Fatalf("expected done CLI with id; got %s", out)
	}
}
