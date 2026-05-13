// Package agents declares the AgentAdapter interface and registers the
// five built-in adapters Samuel v2 ships (claude, codex, copilot,
// gemini, kiro). External agent plugins implement the same interface.
//
// Adapters are declarative — they describe how to invoke the CLI tool
// (prompt-mode, default args, image, env allowlist) but the actual
// sandbox / process spawning lives in internal/sandbox. The
// Adapter.Invoke method on the built-ins routes through that package.
package agents

import (
	"context"
	"fmt"
	"sync"
)

// PromptMode tells the adapter how the agent expects its prompt:
//
//   - PromptModeContentArg : `tool -p "<content>"`
//   - PromptModeFileArg    : `tool --prompt-file <path>`
//   - PromptModeStdin      : the prompt is piped on stdin
type PromptMode string

const (
	PromptModeContentArg PromptMode = "content-arg"
	PromptModeFileArg    PromptMode = "file-arg"
	PromptModeStdin      PromptMode = "stdin-content"
)

// Manifest is the declarative descriptor an adapter exports. Plugins
// would serialize the same fields in samuel-plugin.toml.
type Manifest struct {
	Name         string
	DefaultImage string
	EnvAllowlist []string
	PromptMode   PromptMode
	DefaultArgs  []string
}

// Options control one Invoke call.
type Options struct {
	ProjectDir string
	// PromptContent is the rendered iteration prompt.
	PromptContent string
	// PromptPath is the on-disk location of the prompt file (used by
	// adapters in PromptModeFileArg).
	PromptPath string
	// Sandbox is "none" or "oci" — passed through so adapter.Invoke can
	// dispatch to the right launcher.
	Sandbox string
	// SandboxImage may override the adapter's DefaultImage.
	SandboxImage string
	// ExtraArgs append to DefaultArgs (callers rarely use this).
	ExtraArgs []string
	// DryRun stops at log-only mode; nothing is actually executed.
	DryRun bool
	// CommandRunner abstracts the host-process invocation so tests can
	// inject mocks. When nil the real adapter wires the OCI sandbox /
	// host exec path.
	CommandRunner CommandRunner
}

// CommandRunner is the abstract invocation surface. The real
// implementation lives in internal/sandbox; tests inject mocks.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, opts CommandOptions) (Result, error)
}

// CommandOptions carries everything the sandbox/exec layer needs to
// build the host command.
type CommandOptions struct {
	WorkDir      string
	Stdin        string
	EnvAllowlist []string
	Sandbox      string
	SandboxImage string
}

// Result is the return value of one adapter invocation.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// AgentAdapter is the abstract interface every agent — built-in or
// plugin-provided — implements.
type AgentAdapter interface {
	Manifest() Manifest
	BuildArgs(opts Options) []string
	Invoke(ctx context.Context, opts Options) (Result, error)
}

// builtinAdapter is the concrete struct backing the five shipped
// adapters. The behaviour is uniform; only the manifest values differ.
type builtinAdapter struct {
	manifest Manifest
	// buildArgs lets each adapter customize the args slice past the
	// declarative defaults (e.g. claude wraps the prompt in `-p`).
	buildArgs func(m Manifest, opts Options) []string
}

func (a *builtinAdapter) Manifest() Manifest { return a.manifest }

func (a *builtinAdapter) BuildArgs(opts Options) []string {
	if a.buildArgs != nil {
		return a.buildArgs(a.manifest, opts)
	}
	args := append([]string{}, a.manifest.DefaultArgs...)
	args = append(args, opts.ExtraArgs...)
	return args
}

// Invoke spawns the configured CLI tool with the rendered prompt. For
// content-arg adapters the prompt is folded into BuildArgs; for stdin
// it goes through CommandOptions.Stdin; for file-arg the prompt file
// path is the argument.
func (a *builtinAdapter) Invoke(ctx context.Context, opts Options) (Result, error) {
	if opts.CommandRunner == nil {
		return Result{}, fmt.Errorf("agent %q: no CommandRunner set", a.manifest.Name)
	}
	args := a.BuildArgs(opts)
	stdin := ""
	if a.manifest.PromptMode == PromptModeStdin {
		stdin = opts.PromptContent
	}
	cmdOpts := CommandOptions{
		WorkDir:      opts.ProjectDir,
		Stdin:        stdin,
		EnvAllowlist: a.manifest.EnvAllowlist,
		Sandbox:      opts.Sandbox,
		SandboxImage: opts.SandboxImage,
	}
	if cmdOpts.SandboxImage == "" {
		cmdOpts.SandboxImage = a.manifest.DefaultImage
	}
	if opts.DryRun {
		return Result{Stdout: fmt.Sprintf("[dry-run] %s %v", a.manifest.Name, args)}, nil
	}
	return opts.CommandRunner.Run(ctx, a.manifest.Name, args, cmdOpts)
}

// Registry holds the active adapter set. The package init function
// registers the five built-ins; plugins register additional adapters
// via Register.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]AgentAdapter
}

// global is the package-level Registry. CLI code looks adapters up
// through Get / List.
var global = &Registry{adapters: map[string]AgentAdapter{}}

// Register installs adapter under its manifest name. Subsequent calls
// for the same name replace the prior entry — last-wins semantics so
// plugins can shadow built-ins when configured to.
func Register(a AgentAdapter) {
	global.mu.Lock()
	defer global.mu.Unlock()
	global.adapters[a.Manifest().Name] = a
}

// Get returns the adapter registered under name.
func Get(name string) (AgentAdapter, bool) {
	global.mu.RLock()
	defer global.mu.RUnlock()
	a, ok := global.adapters[name]
	return a, ok
}

// List returns the names of every registered adapter, sorted by Name
// for stable CLI output. The slice is a copy and safe to mutate.
func List() []string {
	global.mu.RLock()
	defer global.mu.RUnlock()
	names := make([]string, 0, len(global.adapters))
	for n := range global.adapters {
		names = append(names, n)
	}
	return names
}

// Default returns the canonical default adapter name. Used when
// samuel.toml omits the methodology's `agent` field.
func Default() string { return "claude" }

// IsValid reports whether name resolves to a registered adapter.
func IsValid(name string) bool {
	_, ok := Get(name)
	return ok
}
