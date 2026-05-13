package ralph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ar4mirez/samuel/internal/agents"
	"github.com/ar4mirez/samuel/internal/methodology/hooks"
	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
)

// fakePlugin is a minimal Hook handler that lets composition tests
// record invocation order without spinning up the full plugin loader.
type fakePlugin struct {
	name string
	log  *[]string
	err  error
}

func (f *fakePlugin) Name() hooks.HookName { return hooks.ContextSnapshot }

func (f *fakePlugin) Run(_ context.Context, _ hooks.HookInput, _ *hooks.HookOutput) error {
	*f.log = append(*f.log, f.name)
	return f.err
}

func TestHookComposition_TwoPlugins_PlusDefault(t *testing.T) {
	dir, _ := setupRalphProject(t)
	registry := hooks.NewRegistry()
	RegisterDefaults(registry)

	var log []string
	registry.RegisterWithOrder(&fakePlugin{name: "pluginA", log: &log}, hooks.SourcePlugin, "A", 200)
	registry.RegisterWithOrder(&fakePlugin{name: "pluginB", log: &log}, hooks.SourcePlugin, "B", 210)

	adapter, _ := agents.Get("claude")
	cfg := LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 1,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        &fakeRunner{},
	}
	if err := RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("loop: %v", err)
	}
	// Both plugin handlers should fire on context.snapshot, in order.
	if len(log) < 2 || log[0] != "pluginA" || log[1] != "pluginB" {
		t.Fatalf("plugin chain didn't run in order; got %v", log)
	}
}

func TestStrictMode_QualityCheckAborts(t *testing.T) {
	dir, _ := setupRalphProject(t)
	registry := hooks.NewRegistry()
	RegisterDefaults(registry)
	registry.SetConfig(hooks.QualityCheck, hooks.Config{Strict: true})
	registry.RegisterWithOrder(hooks.Func{
		HookName: hooks.QualityCheck,
		Fn: func(_ context.Context, _ hooks.HookInput, _ *hooks.HookOutput) error {
			return errors.New("simulated failure")
		},
	}, hooks.SourcePlugin, "checker", 50)

	adapter, _ := agents.Get("claude")
	cfg := LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 3,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        &fakeRunner{},
	}
	err := RunAutoLoop(context.Background(), cfg)
	if err == nil {
		t.Fatal("strict quality.check should abort the loop")
	}
	if !hooks.IsHookError(err) {
		t.Fatalf("expected HookError, got %T", err)
	}
}

func TestNonStrictMode_LogsWarningContinues(t *testing.T) {
	dir, _ := setupRalphProject(t)
	registry := hooks.NewRegistry()
	RegisterDefaults(registry)
	registry.SetConfig(hooks.ContextExtra, hooks.Config{Strict: false})
	registry.Register(hooks.Func{
		HookName: hooks.ContextExtra,
		Fn: func(_ context.Context, _ hooks.HookInput, _ *hooks.HookOutput) error {
			return errors.New("non-fatal")
		},
	}, hooks.SourcePlugin)

	adapter, _ := agents.Get("claude")
	cfg := LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 1,
		Adapter:       adapter,
		Hooks:         registry,
		Runner:        &fakeRunner{},
	}
	if err := RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("non-strict warning should not abort loop: %v", err)
	}
	warns := registry.TakeWarnings()
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "non-fatal") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected non-fatal warning recorded; got %v", warns)
	}
}

func TestMultiAgentSwap_SwitchClaudeToCodex_LoopContinues(t *testing.T) {
	dir, _ := setupRalphProject(t)
	p, _, _ := prd.Load(prd.PRDPath(dir))
	// Start with claude
	p.Config.AITool = "claude"
	_ = p.Save(prd.PRDPath(dir))

	registry := hooks.NewRegistry()
	RegisterDefaults(registry)
	runner := &countingRunner{}
	cfg := LoopConfig{
		ProjectDir:    dir,
		PRDPath:       prd.PRDPath(dir),
		MaxIterations: 1,
		Hooks:         registry,
		Runner:        runner,
	}
	if err := RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("claude iteration: %v", err)
	}
	firstAgent := runner.lastAgent

	// Swap to codex and run again
	p, _, _ = prd.Load(prd.PRDPath(dir))
	p.Config.AITool = "codex"
	_ = p.Save(prd.PRDPath(dir))
	cfg.MaxIterations = 1
	if err := RunAutoLoop(context.Background(), cfg); err != nil {
		t.Fatalf("codex iteration: %v", err)
	}
	if runner.lastAgent == firstAgent {
		t.Fatalf("agent should have swapped; both runs used %q", runner.lastAgent)
	}
}

type countingRunner struct {
	lastAgent string
	calls     int
}

func (c *countingRunner) Run(_ context.Context, name string, _ []string, _ agents.CommandOptions) (agents.Result, error) {
	c.lastAgent = name
	c.calls++
	return agents.Result{Stdout: fmt.Sprintf("%s ok", name)}, nil
}

func TestTemplateOverride_ProjectShadowsEmbedded(t *testing.T) {
	dir, _ := setupRalphProject(t)
	overrideDir := filepath.Join(dir, ".samuel", "templates", "ralph")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideDir, "prompt.md.tmpl"),
		[]byte("OVERRIDE\nsamuel run done {{ .Iteration.TaskID }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _, _ := prd.Load(prd.PRDPath(dir))
	out, err := renderPrompt(dir, p, 1, p.GetNextTask(), hooks.IterationTypeImplementation)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "OVERRIDE") {
		t.Fatalf("expected override prompt; got %s", out)
	}
}
