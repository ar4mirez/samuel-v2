// Package ralph wires the built-in Ralph Wiggum methodology: it owns
// the loop driver, the default hook handlers (snapshot/progress/task
// context generators, the agent.invoke adapter dispatcher, and the
// iteration gate that delegates to pilot mode).
//
// The whole methodology is "built-in" — it lives in the framework
// binary so a fresh install can run `samuel run start` without any
// plugin. RFD 0004 / 0006 define the contract; this package is the
// implementation.
package ralph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ar4mirez/samuel/internal/agents"
	rctx "github.com/ar4mirez/samuel/internal/methodology/ralph/context"
	"github.com/ar4mirez/samuel/internal/methodology/hooks"
	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
	"github.com/ar4mirez/samuel/internal/methodology/ralph/templates"
	"github.com/ar4mirez/samuel/internal/sandbox"
)

// LoopConfig parameterizes one RunAutoLoop invocation.
type LoopConfig struct {
	ProjectDir     string
	PRDPath        string
	MaxIterations  int
	PauseSecs      int
	MaxConsecFails int
	DryRun         bool
	Profile        bool
	// Adapter pins the agent adapter to use. When empty the loop pulls
	// the name from the PRD's Config.AITool.
	Adapter agents.AgentAdapter
	// Hooks is the resolved registry — the caller (CLI handler) builds
	// it from samuel.toml and any plugin handlers.
	Hooks *hooks.Registry
	// Runner is the sandbox / host-exec implementation; tests inject
	// mocks here.
	Runner agents.CommandRunner
	// OnIterStart / OnIterEnd are optional callbacks for the CLI UI.
	OnIterStart func(iter int, iterType string, task *prd.AutoTask)
	OnIterEnd   func(iter int, err error)
}

// NewLoopConfig hydrates a LoopConfig with the defaults that match v1.
// PAUSE_SECONDS and MAX_CONSECUTIVE_FAILURES env vars override the
// PRD-level values when set.
func NewLoopConfig(projectDir string, p *prd.AutoPRD) LoopConfig {
	pauseSecs := prd.DefaultPauseSecs
	if val := os.Getenv("PAUSE_SECONDS"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			pauseSecs = parsed
		}
	}
	maxConsecFails := prd.DefaultMaxConsecFails
	if val := os.Getenv("MAX_CONSECUTIVE_FAILURES"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			maxConsecFails = parsed
		}
	}
	return LoopConfig{
		ProjectDir:     projectDir,
		PRDPath:        prd.PRDPath(projectDir),
		MaxIterations:  p.Config.MaxIterations,
		PauseSecs:      pauseSecs,
		MaxConsecFails: maxConsecFails,
	}
}

// RunAutoLoop drives the autonomous loop using the configured
// methodology. Each iteration:
//
//  1. Reloads prd.toon (the agent may have mutated it via CLI).
//  2. Fires before:iteration hook.
//  3. Asks iteration.gate whether this is impl or discovery.
//  4. Regenerates pre-computed context (snapshot/progress/task).
//  5. Fires before:agent.invoke, then agent.invoke.
//  6. Fires after:agent.invoke + quality.check.
//  7. Fires after:iteration.
//  8. Pauses PauseSecs and loops.
func RunAutoLoop(ctx context.Context, cfg LoopConfig) error {
	if cfg.Hooks == nil {
		return errors.New("ralph: LoopConfig.Hooks is required")
	}
	if cfg.Runner == nil {
		cfg.Runner = sandbox.New(cfg.ProjectDir)
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 1
	}
	if cfg.MaxConsecFails <= 0 {
		cfg.MaxConsecFails = prd.DefaultMaxConsecFails
	}
	cfg.Hooks.EnableProfile(cfg.Profile)

	if _, err := cfg.Hooks.Run(ctx, hooks.BeforeLoop, hooks.HookInput{
		ProjectDir:    cfg.ProjectDir,
		RunDir:        prd.RunPath(cfg.ProjectDir),
		CurrentIteration: 0,
	}, hooks.AllowAll); err != nil {
		return err
	}

	consecutiveFailures := 0
	lastDiscovery := 0
	for i := 1; i <= cfg.MaxIterations; i++ {
		p, _, err := prd.Load(cfg.PRDPath)
		if err != nil {
			return fmt.Errorf("iteration %d: reload prd.toon: %w", i, err)
		}
		next := p.GetNextTask()
		iterType := hooks.IterationTypeImplementation
		if cfg.shouldDiscover(p, i, lastDiscovery) {
			iterType = hooks.IterationTypeDiscovery
		}
		gateOut, err := cfg.Hooks.Run(ctx, hooks.IterationGate, hooks.HookInput{
			ProjectDir:       cfg.ProjectDir,
			RunDir:           prd.RunPath(cfg.ProjectDir),
			CurrentIteration: i,
			IterationType:    iterType,
		}, hooks.AllowAll)
		if err != nil {
			return err
		}
		if gateOut.IterationType != "" {
			iterType = gateOut.IterationType
		}
		if next == nil && iterType == hooks.IterationTypeImplementation {
			// Queue empty — end the loop cleanly.
			notifyIterEnd(cfg.OnIterEnd, i, nil)
			break
		}

		notifyIterStart(cfg.OnIterStart, i, iterType, next)

		input := hooks.HookInput{
			ProjectDir:       cfg.ProjectDir,
			RunDir:           prd.RunPath(cfg.ProjectDir),
			CurrentIteration: i,
			IterationType:    iterType,
			Payload:          map[string]any{},
		}
		if next != nil {
			input.CurrentTaskID = next.ID
		}

		if _, err := cfg.Hooks.Run(ctx, hooks.BeforeIteration, input, hooks.AllowAll); err != nil {
			return err
		}
		regenerateContext(cfg.ProjectDir, p, iterType == hooks.IterationTypeDiscovery)

		if _, err := cfg.Hooks.Run(ctx, hooks.ContextSnapshot, input, hooks.AllowAll); err != nil {
			return err
		}
		if _, err := cfg.Hooks.Run(ctx, hooks.ContextProgress, input, hooks.AllowAll); err != nil {
			return err
		}
		if _, err := cfg.Hooks.Run(ctx, hooks.ContextTask, input, hooks.AllowAll); err != nil {
			return err
		}
		if _, err := cfg.Hooks.Run(ctx, hooks.ContextExtra, input, hooks.AllowAll); err != nil {
			return err
		}

		prompt, err := renderPrompt(cfg.ProjectDir, p, i, next, iterType)
		if err != nil {
			return fmt.Errorf("iteration %d: render prompt: %w", i, err)
		}
		input.Payload["prompt"] = prompt
		if _, err := cfg.Hooks.Run(ctx, hooks.BeforeAgent, input, hooks.AllowAll); err != nil {
			return err
		}

		agentOut, err := invokeAgent(ctx, cfg, p, prompt, iterType)
		if err != nil {
			consecutiveFailures++
			notifyIterEnd(cfg.OnIterEnd, i, err)
			if consecutiveFailures >= cfg.MaxConsecFails {
				return fmt.Errorf("%d consecutive failures reached — aborting", cfg.MaxConsecFails)
			}
		} else {
			consecutiveFailures = 0
			input.Payload["agent_stdout"] = agentOut.Stdout
			input.Payload["agent_stderr"] = agentOut.Stderr
		}

		if _, err := cfg.Hooks.Run(ctx, hooks.AfterAgent, input, hooks.AllowAll); err != nil {
			return err
		}
		if _, err := cfg.Hooks.Run(ctx, hooks.QualityCheck, input, hooks.AllowAll); err != nil {
			return err
		}
		if _, err := cfg.Hooks.Run(ctx, hooks.AfterIteration, input, hooks.AllowAll); err != nil {
			return err
		}

		recordIterationProgress(cfg.PRDPath, i, iterType)
		if iterType == hooks.IterationTypeDiscovery {
			lastDiscovery = i
		}

		notifyIterEnd(cfg.OnIterEnd, i, nil)
		if i < cfg.MaxIterations && cfg.PauseSecs > 0 {
			select {
			case <-time.After(time.Duration(cfg.PauseSecs) * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	if _, err := cfg.Hooks.Run(ctx, hooks.AfterLoop, hooks.HookInput{
		ProjectDir: cfg.ProjectDir,
		RunDir:     prd.RunPath(cfg.ProjectDir),
	}, hooks.AllowAll); err != nil {
		return err
	}
	return nil
}

func (cfg *LoopConfig) shouldDiscover(p *prd.AutoPRD, currentIter, lastDiscovery int) bool {
	if !p.Config.PilotMode || p.Config.PilotConfig == nil {
		return false
	}
	return ShouldRunDiscovery(p, currentIter, lastDiscovery, p.Config.PilotConfig.DiscoverInterval)
}

// regenerateContext writes the three pre-computed context files
// before the agent.invoke hook fires.
func regenerateContext(projectDir string, p *prd.AutoPRD, isDiscovery bool) {
	_ = rctx.GenerateProjectSnapshot(projectDir)
	rctx.PrepareProgressContext(projectDir, rctx.FromAutoConfig(p.Config))
	_ = rctx.GenerateTaskContext(projectDir, p, isDiscovery)
}

func renderPrompt(projectDir string, p *prd.AutoPRD, iter int, next *prd.AutoTask, iterType string) (string, error) {
	pctx := templates.PromptContext{
		Samuel:      templates.SamuelInfo{Version: "2.0.0-beta.2"},
		Project:     templates.ProjectInfo{Name: p.Project.Name, Description: p.Project.Description},
		Methodology: templates.MethodologyInfo{Name: p.Config.Methodology},
		Iteration:   templates.IterationInfo{Number: iter, Type: iterType},
		Config:      p.Config,
		Guardrails:  templates.GuardrailsInfo{MaxFunctionLines: 50, MaxFileLines: 300, RequireTests: true},
		Paths: templates.PathsInfo{
			PRD:              filepath.Join(prd.RunDir, prd.PRDFile),
			Progress:         filepath.Join(prd.RunDir, prd.ProgressFile),
			ProgressContext:  filepath.Join(prd.RunDir, prd.ProgressContextFile),
			TaskContext:      filepath.Join(prd.RunDir, prd.TaskContextFile),
			ProjectSnapshot:  filepath.Join(prd.RunDir, prd.SnapshotFile),
			AgentsMD:         "AGENTS.md",
		},
		State: templates.StateInfo{
			PendingTasks:    p.CountPendingTasks(),
			CompletedTasks:  p.Progress.CompletedTasks,
			TotalIterations: p.Progress.TotalIterationsRun,
		},
		Mode: iterType,
	}
	if next != nil {
		pctx.Iteration.TaskID = next.ID
	}
	if iterType == hooks.IterationTypeDiscovery {
		return templates.RenderDiscovery(projectDir, pctx)
	}
	return templates.Render(projectDir, pctx)
}

func invokeAgent(ctx context.Context, cfg LoopConfig, p *prd.AutoPRD, prompt, iterType string) (agents.Result, error) {
	adapter := cfg.Adapter
	if adapter == nil {
		got, ok := agents.Get(p.Config.AITool)
		if !ok {
			return agents.Result{}, fmt.Errorf("unknown agent %q (run `samuel doctor` to inspect adapters)", p.Config.AITool)
		}
		adapter = got
	}
	sandboxMode := p.Config.Sandbox
	if cfg.DryRun {
		sandboxMode = sandbox.SandboxDryRun
	}
	return adapter.Invoke(ctx, agents.Options{
		ProjectDir:    cfg.ProjectDir,
		PromptContent: prompt,
		PromptPath:    filepath.Join(cfg.ProjectDir, prd.RunDir, prd.PromptFile),
		Sandbox:       sandboxMode,
		SandboxImage:  p.Config.SandboxImage,
		DryRun:        cfg.DryRun,
		CommandRunner: cfg.Runner,
	})
}

// recordIterationProgress is best-effort — failures to update prd.toon
// don't abort the loop; the agent will reconcile on the next iteration
// (it always reloads).
func recordIterationProgress(prdPath string, iter int, iterType string) {
	p, _, err := prd.Load(prdPath)
	if err != nil {
		return
	}
	p.Progress.CurrentIteration = iter
	p.Progress.TotalIterationsRun = iter
	p.Progress.LastIterationAt = time.Now().UTC().Format(time.RFC3339)
	if p.Progress.Status == prd.LoopStatusNotStarted {
		p.Progress.Status = prd.LoopStatusRunning
	}
	switch iterType {
	case hooks.IterationTypeDiscovery:
		p.Progress.DiscoveryIterations++
	default:
		p.Progress.ImplIterations++
	}
	_ = p.Save(prdPath)
}

func notifyIterStart(fn func(int, string, *prd.AutoTask), iter int, iterType string, task *prd.AutoTask) {
	if fn != nil {
		fn(iter, iterType, task)
	}
}

func notifyIterEnd(fn func(int, error), iter int, err error) {
	if fn != nil {
		fn(iter, err)
	}
}
