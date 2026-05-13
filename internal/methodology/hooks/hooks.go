// Package hooks implements the lifecycle-hook framework that methodologies
// (built-in or plugin-provided) extend the autonomous loop through.
//
// A handler is anything that satisfies Hook.Run. Handlers are registered
// against named hook points (see HookName constants) with an integer
// `order` — lower order runs first. Built-in defaults register at order
// 100; plugin handlers default to order 200; samuel.toml can override
// either via [hooks.<name>.order].
//
// Per-hook configuration is read from samuel.toml [hooks.<name>]:
//
//   - strict   = true   → handler error aborts the iteration
//   - strict   = false  → handler error logs a warning, iteration proceeds
//   - timeout  = "5m"   → per-handler timeout (default 5 minutes)
//
// Defaults follow RFD 0004: quality.check and before:loop are strict;
// every other hook is non-strict (warn-and-continue).
package hooks

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// HookName is a typed string for the 13 lifecycle hook points listed in
// RFD 0004. Using a typed constant set lets the registry reject typos
// at compile time.
type HookName string

const (
	BeforeLoop      HookName = "before:loop"
	AfterLoop       HookName = "after:loop"
	BeforeIteration HookName = "before:iteration"
	AfterIteration  HookName = "after:iteration"
	IterationGate   HookName = "iteration.gate"
	ContextSnapshot HookName = "context.snapshot"
	ContextProgress HookName = "context.progress"
	ContextTask     HookName = "context.task"
	ContextExtra    HookName = "context.extra"
	BeforeAgent     HookName = "before:agent.invoke"
	AgentInvoke     HookName = "agent.invoke"
	AfterAgent      HookName = "after:agent.invoke"
	QualityCheck    HookName = "quality.check"
)

// AllHookNames lists every defined hook point in lifecycle order.
func AllHookNames() []HookName {
	return []HookName{
		BeforeLoop, AfterLoop,
		BeforeIteration, AfterIteration,
		IterationGate,
		ContextSnapshot, ContextProgress, ContextTask, ContextExtra,
		BeforeAgent, AgentInvoke, AfterAgent,
		QualityCheck,
	}
}

// IsValid reports whether s is one of the defined hook names.
func IsValid(s HookName) bool {
	for _, n := range AllHookNames() {
		if n == s {
			return true
		}
	}
	return false
}

// HookInput is what the loop hands to every handler call. Fields are
// optional — different hooks populate different fields (the loop sets
// CurrentIteration on every call but only sets CurrentTaskID for hooks
// that fire mid-iteration).
type HookInput struct {
	ProjectDir       string
	RunDir           string
	HookName         HookName
	CurrentIteration int
	IterationType    string
	CurrentTaskID    string
	// Payload is hook-specific scratch. before:agent.invoke uses it to
	// carry the generated prompt; agent.invoke uses it to receive
	// adapter args. Handlers that don't care about it ignore the field.
	Payload map[string]any
}

// HookOutput is what a handler may return to influence the next stage.
// Most handlers do not need to return anything beyond err.
//
// Special semantics:
//   - For IterationGate: setting IterationType=IterationTypeDiscovery
//     or =IterationTypeImplementation overrides the loop default.
//   - For AgentInvoke: returning Output as agent stdout / stderr lets
//     the loop persist it.
//   - Mutations to Payload are propagated to subsequent handlers in
//     the chain (handlers are run in deterministic order).
type HookOutput struct {
	IterationType string
	Output        string
	Payload       map[string]any
}

// Iteration type constants — kept here so hook callers do not depend
// on the methodology package.
const (
	IterationTypeImplementation = "implementation"
	IterationTypeDiscovery      = "discovery"
)

// Hook is the handler interface plugins (and built-ins) implement.
// Each handler does one thing for one hook point.
type Hook interface {
	Name() HookName
	Run(ctx context.Context, in HookInput, out *HookOutput) error
}

// Func is a convenience adapter so handlers can be written as plain
// functions instead of struct types.
type Func struct {
	HookName HookName
	Fn       func(ctx context.Context, in HookInput, out *HookOutput) error
}

func (f Func) Name() HookName { return f.HookName }

func (f Func) Run(ctx context.Context, in HookInput, out *HookOutput) error {
	if f.Fn == nil {
		return nil
	}
	return f.Fn(ctx, in, out)
}

// Source identifies where a handler came from (built-in default,
// plugin name, user override). It lets the registry produce
// deterministic ordering when two handlers share the same order.
type Source string

const (
	SourceDefault Source = "default"
	SourceUser    Source = "user"
	SourcePlugin  Source = "plugin"
)

// Entry is one registered handler with its resolved order, source, and
// — for plugin-provided handlers — the originating plugin name.
type Entry struct {
	Hook    Hook
	Order   int
	Source  Source
	Plugin  string
	Strict  *bool         // pointer = "unset, fall back to defaults"
	Timeout time.Duration // zero = use registry default
}

// Config controls per-hook strict/timeout behaviour. The registry holds
// one Config per hook name; samuel.toml overrides land here at load
// time.
type Config struct {
	Strict  bool
	Timeout time.Duration
	// OrderOverride lets samuel.toml force a specific handler to a
	// specific order regardless of its registration source.
	OrderOverride map[string]int
}

// DefaultTimeout is the per-handler timeout when nothing else is set.
const DefaultTimeout = 5 * time.Minute

// DefaultStrictHooks lists the hook points that abort the loop on
// handler error by default. Other hooks warn-and-continue.
func DefaultStrictHooks() map[HookName]bool {
	return map[HookName]bool{
		BeforeLoop:   true,
		QualityCheck: true,
	}
}

// Registry holds every registered handler grouped by hook name, with
// per-hook configuration overlays.
type Registry struct {
	mu       sync.RWMutex
	entries  map[HookName][]Entry
	config   map[HookName]Config
	profile  bool
	warnings []Warning
	timings  []Timing
}

// Warning describes a non-fatal event the loop should surface to the
// user (progress.md entry, status output) without aborting.
type Warning struct {
	Hook    HookName
	Plugin  string
	Message string
	At      time.Time
}

// Timing is captured per handler when the registry runs in --profile
// mode. The loop emits these as `[hooks.timing]` lines to progress.md.
type Timing struct {
	Hook     HookName
	Plugin   string
	Order    int
	Duration time.Duration
	Err      error
}

// NewRegistry returns a Registry with default config for every defined
// hook name.
func NewRegistry() *Registry {
	r := &Registry{
		entries: make(map[HookName][]Entry),
		config:  make(map[HookName]Config),
	}
	for _, n := range AllHookNames() {
		r.config[n] = Config{
			Strict:        DefaultStrictHooks()[n],
			Timeout:       DefaultTimeout,
			OrderOverride: map[string]int{},
		}
	}
	return r
}

// EnableProfile toggles --profile mode. When on, Run accumulates a
// Timing entry per handler invocation; callers retrieve them via
// Timings() at iteration end.
func (r *Registry) EnableProfile(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profile = on
}

// SetConfig overlays per-hook strict / timeout / order configuration.
// Typically called once after samuel.toml load.
func (r *Registry) SetConfig(name HookName, cfg Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !IsValid(name) {
		return
	}
	merged := r.config[name]
	merged.Strict = cfg.Strict
	if cfg.Timeout > 0 {
		merged.Timeout = cfg.Timeout
	}
	if cfg.OrderOverride != nil {
		if merged.OrderOverride == nil {
			merged.OrderOverride = map[string]int{}
		}
		for k, v := range cfg.OrderOverride {
			merged.OrderOverride[k] = v
		}
	}
	r.config[name] = merged
}

// Register attaches a handler to its hook (Hook.Name) with the given
// source. For SourceDefault the order is 100; SourcePlugin defaults to
// 200; SourceUser defaults to 50 (overrides always win).
//
// Use RegisterWithOrder to force a specific order explicitly.
func (r *Registry) Register(h Hook, source Source) {
	order := 200
	switch source {
	case SourceDefault:
		order = 100
	case SourceUser:
		order = 50
	}
	r.RegisterWithOrder(h, source, "", order)
}

// RegisterWithOrder is the explicit-order variant. plugin may be empty
// for built-in defaults.
func (r *Registry) RegisterWithOrder(h Hook, source Source, plugin string, order int) {
	if h == nil || !IsValid(h.Name()) {
		return
	}
	e := Entry{Hook: h, Order: order, Source: source, Plugin: plugin}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Apply samuel.toml override if present.
	if cfg, ok := r.config[h.Name()]; ok && plugin != "" {
		if ord, ok := cfg.OrderOverride[plugin]; ok {
			e.Order = ord
		}
	}
	r.entries[h.Name()] = append(r.entries[h.Name()], e)
}

// Replace removes every existing handler for `name` and installs `h` as
// the sole built-in default. Useful when the loop swaps the default
// `agent.invoke` adapter between iterations.
func (r *Registry) Replace(name HookName, h Hook) {
	r.mu.Lock()
	r.entries[name] = nil
	r.mu.Unlock()
	r.Register(h, SourceDefault)
}

// Handlers returns the resolved-and-ordered handler list for `name`.
// Order: ascending by Entry.Order; ties broken by Source priority
// (User < Default < Plugin) so user overrides win deterministically.
func (r *Registry) Handlers(name HookName) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src := r.entries[name]
	out := make([]Entry, len(src))
	copy(out, src)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return sourcePriority(out[i].Source) < sourcePriority(out[j].Source)
	})
	return out
}

func sourcePriority(s Source) int {
	switch s {
	case SourceUser:
		return 0
	case SourceDefault:
		return 1
	case SourcePlugin:
		return 2
	}
	return 3
}

// CapabilityChecker is a hook-time gate per RFD 0004 resolution #6:
// before invoking a plugin-provided handler, the registry asks whether
// the plugin still holds the capabilities its manifest declared. Tests
// inject AllowAll; production wires it to capability.Match.
type CapabilityChecker func(plugin string, hook HookName) error

// AllowAll grants every capability check. Used in tests and for
// built-in handlers (which never go through plugin-cap gating).
var AllowAll CapabilityChecker = func(string, HookName) error { return nil }

// DenyAll refuses every check. Used by the strict-mode tests to confirm
// a denied handler doesn't run.
var DenyAll CapabilityChecker = func(p string, h HookName) error {
	return fmt.Errorf("capability denied for plugin %q on hook %q", p, h)
}

// Run invokes every handler bound to `name` in order. The output value
// flows through the chain so handlers can build on each other. The
// returned error is non-nil only when a strict handler failed.
//
// Non-strict failures populate Warnings (retrieve via TakeWarnings)
// and, when profile mode is on, Timings.
func (r *Registry) Run(ctx context.Context, name HookName, in HookInput, capCheck CapabilityChecker) (HookOutput, error) {
	if capCheck == nil {
		capCheck = AllowAll
	}
	in.HookName = name
	handlers := r.Handlers(name)
	cfg := r.snapshotConfig(name)

	var out HookOutput
	out.Payload = in.Payload
	for _, e := range handlers {
		if e.Plugin != "" {
			if err := capCheck(e.Plugin, name); err != nil {
				r.recordWarning(name, e.Plugin, fmt.Sprintf("skipped (capability denied): %v", err))
				if cfg.Strict {
					return out, &HookError{Hook: name, Plugin: e.Plugin, Cause: err, Strict: true}
				}
				continue
			}
		}
		hctx, cancel := r.withTimeout(ctx, e, cfg.Timeout)
		start := time.Now()
		err := e.Hook.Run(hctx, in, &out)
		dur := time.Since(start)
		cancel()
		r.maybeRecordTiming(Timing{Hook: name, Plugin: e.Plugin, Order: e.Order, Duration: dur, Err: err})
		if err == nil {
			continue
		}
		strict := cfg.Strict
		if e.Strict != nil {
			strict = *e.Strict
		}
		if strict {
			return out, &HookError{Hook: name, Plugin: e.Plugin, Cause: err, Strict: true}
		}
		r.recordWarning(name, e.Plugin, err.Error())
	}
	return out, nil
}

func (r *Registry) snapshotConfig(name HookName) Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config[name]
}

func (r *Registry) withTimeout(parent context.Context, e Entry, def time.Duration) (context.Context, context.CancelFunc) {
	t := e.Timeout
	if t <= 0 {
		t = def
	}
	if t <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, t)
}

func (r *Registry) recordWarning(name HookName, plugin, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warnings = append(r.warnings, Warning{Hook: name, Plugin: plugin, Message: msg, At: time.Now()})
}

func (r *Registry) maybeRecordTiming(t Timing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.profile {
		return
	}
	r.timings = append(r.timings, t)
}

// timings is recorded only when EnableProfile(true) was set. Kept as a
// separate slice so the warning slice stays cheap to mutate in the hot
// path.
func (r *Registry) Timings() []Timing {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Timing, len(r.timings))
	copy(out, r.timings)
	return out
}

// TakeWarnings drains the accumulated warning slice and returns it to
// the caller. After this call the registry's warning buffer is empty.
func (r *Registry) TakeWarnings() []Warning {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.warnings
	r.warnings = nil
	return out
}

// HookError wraps a handler failure with the hook + plugin context the
// caller needs to render a useful warning.
type HookError struct {
	Hook   HookName
	Plugin string
	Cause  error
	Strict bool
}

func (e *HookError) Error() string {
	if e.Plugin != "" {
		return fmt.Sprintf("hook %s (plugin %s) failed: %v", e.Hook, e.Plugin, e.Cause)
	}
	return fmt.Sprintf("hook %s failed: %v", e.Hook, e.Cause)
}

func (e *HookError) Unwrap() error { return e.Cause }

// IsHookError reports whether err originated from the registry.
func IsHookError(err error) bool {
	var h *HookError
	return errors.As(err, &h)
}
