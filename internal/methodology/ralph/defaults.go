package ralph

import (
	"context"
	"fmt"

	rctx "github.com/ar4mirez/samuel/internal/methodology/ralph/context"
	"github.com/ar4mirez/samuel/internal/methodology/hooks"
	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
)

// RegisterDefaults installs the built-in handlers for every hook
// point Ralph cares about. CLI handlers call this once per invocation,
// then layer plugin-provided handlers on top via Registry.Register.
//
// The default handlers regenerate the pre-computed context files and
// gate impl/discovery iterations. agent.invoke is dispatched directly
// by the loop driver (RunAutoLoop.invokeAgent) so it does not need a
// default hook handler here.
func RegisterDefaults(r *hooks.Registry) {
	r.Register(hooks.Func{
		HookName: hooks.ContextSnapshot,
		Fn: func(_ context.Context, in hooks.HookInput, _ *hooks.HookOutput) error {
			return rctx.GenerateProjectSnapshot(in.ProjectDir)
		},
	}, hooks.SourceDefault)

	r.Register(hooks.Func{
		HookName: hooks.ContextProgress,
		Fn: func(_ context.Context, in hooks.HookInput, _ *hooks.HookOutput) error {
			rctx.PrepareProgressContext(in.ProjectDir, rctx.ProgressConfig{})
			return nil
		},
	}, hooks.SourceDefault)

	r.Register(hooks.Func{
		HookName: hooks.ContextTask,
		Fn: func(_ context.Context, in hooks.HookInput, _ *hooks.HookOutput) error {
			p, _, err := prd.Load(prd.PRDPath(in.ProjectDir))
			if err != nil {
				return err
			}
			isDiscovery := in.IterationType == hooks.IterationTypeDiscovery
			return rctx.GenerateTaskContext(in.ProjectDir, p, isDiscovery)
		},
	}, hooks.SourceDefault)

	r.Register(hooks.Func{
		HookName: hooks.IterationGate,
		Fn: func(_ context.Context, in hooks.HookInput, out *hooks.HookOutput) error {
			p, _, err := prd.Load(prd.PRDPath(in.ProjectDir))
			if err != nil {
				return err
			}
			if !p.Config.PilotMode || p.Config.PilotConfig == nil {
				return nil
			}
			lastDiscovery := 0
			if p.Progress.DiscoveryIterations > 0 {
				lastDiscovery = in.CurrentIteration - 1
			}
			if ShouldRunDiscovery(p, in.CurrentIteration, lastDiscovery, p.Config.PilotConfig.DiscoverInterval) {
				out.IterationType = hooks.IterationTypeDiscovery
			} else {
				out.IterationType = hooks.IterationTypeImplementation
			}
			return nil
		},
	}, hooks.SourceDefault)

	// quality.check default is a no-op — every project wires its own
	// commands through samuel.toml. The hook still fires so plugins
	// can attach their own checkers.
	r.Register(hooks.Func{
		HookName: hooks.QualityCheck,
		Fn: func(_ context.Context, _ hooks.HookInput, _ *hooks.HookOutput) error {
			return nil
		},
	}, hooks.SourceDefault)

	// before:loop default just verifies the run directory is present.
	r.Register(hooks.Func{
		HookName: hooks.BeforeLoop,
		Fn: func(_ context.Context, in hooks.HookInput, _ *hooks.HookOutput) error {
			if in.ProjectDir == "" {
				return fmt.Errorf("before:loop: project directory required")
			}
			return nil
		},
	}, hooks.SourceDefault)
}
