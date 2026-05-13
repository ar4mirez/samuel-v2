package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func recordingHook(name HookName, tag string, log *[]string, errOut error) Hook {
	return Func{
		HookName: name,
		Fn: func(_ context.Context, _ HookInput, _ *HookOutput) error {
			*log = append(*log, tag)
			return errOut
		},
	}
}

func TestRegistry_Run_OrdersByOrderThenSource(t *testing.T) {
	r := NewRegistry()
	var log []string
	r.RegisterWithOrder(recordingHook(ContextSnapshot, "user", &log, nil), SourceUser, "", 50)
	r.RegisterWithOrder(recordingHook(ContextSnapshot, "default", &log, nil), SourceDefault, "", 100)
	r.RegisterWithOrder(recordingHook(ContextSnapshot, "pluginA", &log, nil), SourcePlugin, "A", 200)
	r.RegisterWithOrder(recordingHook(ContextSnapshot, "pluginB", &log, nil), SourcePlugin, "B", 200)

	if _, err := r.Run(context.Background(), ContextSnapshot, HookInput{}, AllowAll); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"user", "default", "pluginA", "pluginB"}
	if strings.Join(log, ",") != strings.Join(want, ",") {
		t.Fatalf("order mismatch:\n  got  %v\n  want %v", log, want)
	}
}

func TestRegistry_StrictAbortsLoop(t *testing.T) {
	r := NewRegistry()
	r.SetConfig(QualityCheck, Config{Strict: true, Timeout: time.Second})
	failure := errors.New("boom")
	r.RegisterWithOrder(recordingHook(QualityCheck, "x", new([]string), failure), SourceDefault, "", 100)
	_, err := r.Run(context.Background(), QualityCheck, HookInput{}, AllowAll)
	if err == nil {
		t.Fatal("expected strict-mode error")
	}
	if !IsHookError(err) {
		t.Fatalf("expected HookError, got %T", err)
	}
}

func TestRegistry_NonStrictWarnsAndContinues(t *testing.T) {
	r := NewRegistry()
	r.SetConfig(ContextExtra, Config{Strict: false, Timeout: time.Second})
	var log []string
	r.RegisterWithOrder(recordingHook(ContextExtra, "first", &log, errors.New("nope")), SourceDefault, "", 100)
	r.RegisterWithOrder(recordingHook(ContextExtra, "second", &log, nil), SourceDefault, "", 110)

	out, err := r.Run(context.Background(), ContextExtra, HookInput{}, AllowAll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log) != 2 {
		t.Fatalf("second handler should still run; got log=%v", log)
	}
	warnings := r.TakeWarnings()
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "nope") {
		t.Fatalf("expected one warning containing 'nope', got %#v", warnings)
	}
	_ = out
}

func TestRegistry_TimeoutFiresAsError(t *testing.T) {
	r := NewRegistry()
	r.SetConfig(QualityCheck, Config{Strict: true, Timeout: 10 * time.Millisecond})
	r.Register(Func{
		HookName: QualityCheck,
		Fn: func(ctx context.Context, _ HookInput, _ *HookOutput) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				return nil
			}
		},
	}, SourceDefault)
	_, err := r.Run(context.Background(), QualityCheck, HookInput{}, AllowAll)
	if err == nil {
		t.Fatal("expected timeout-driven error")
	}
}

func TestRegistry_CapabilityCheckBlocksPluginHandler(t *testing.T) {
	r := NewRegistry()
	var log []string
	r.RegisterWithOrder(recordingHook(ContextSnapshot, "denied", &log, nil), SourcePlugin, "denied-plugin", 200)
	r.RegisterWithOrder(recordingHook(ContextSnapshot, "ok", &log, nil), SourceDefault, "", 100)

	if _, err := r.Run(context.Background(), ContextSnapshot, HookInput{}, DenyAll); err != nil {
		t.Fatalf("non-strict deny should not fail: %v", err)
	}
	if len(log) != 1 || log[0] != "ok" {
		t.Fatalf("denied handler ran; log=%v", log)
	}
	if len(r.TakeWarnings()) == 0 {
		t.Fatal("expected a capability-denied warning")
	}
}

func TestRegistry_ProfileEmitsTimings(t *testing.T) {
	r := NewRegistry()
	r.EnableProfile(true)
	r.Register(Func{HookName: BeforeIteration, Fn: func(context.Context, HookInput, *HookOutput) error { return nil }}, SourceDefault)
	if _, err := r.Run(context.Background(), BeforeIteration, HookInput{}, AllowAll); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(r.Timings()) != 1 {
		t.Fatalf("expected one timing, got %d", len(r.Timings()))
	}
}

func TestRegistry_IterationGateRespectsOutputType(t *testing.T) {
	r := NewRegistry()
	r.Register(Func{
		HookName: IterationGate,
		Fn: func(_ context.Context, _ HookInput, out *HookOutput) error {
			out.IterationType = IterationTypeDiscovery
			return nil
		},
	}, SourceDefault)
	out, err := r.Run(context.Background(), IterationGate, HookInput{}, AllowAll)
	if err != nil {
		t.Fatal(err)
	}
	if out.IterationType != IterationTypeDiscovery {
		t.Fatalf("iteration type not propagated; got %q", out.IterationType)
	}
}

func TestRegistry_OrderOverrideViaConfig(t *testing.T) {
	r := NewRegistry()
	cfg := Config{Strict: false, Timeout: time.Second, OrderOverride: map[string]int{"late": 50}}
	r.SetConfig(ContextSnapshot, cfg)
	var log []string
	r.RegisterWithOrder(recordingHook(ContextSnapshot, "default", &log, nil), SourceDefault, "", 100)
	// Plugin "late" would normally be 200; samuel.toml pushes it to 50.
	r.Register(Func{HookName: ContextSnapshot, Fn: func(_ context.Context, _ HookInput, _ *HookOutput) error {
		log = append(log, "late")
		return nil
	}}, SourcePlugin)
	// Plugin path inside Register defaults to plugin="" — so wire it through RegisterWithOrder for explicit plugin name.
	r.RegisterWithOrder(Func{HookName: ContextSnapshot, Fn: func(_ context.Context, _ HookInput, _ *HookOutput) error {
		log = append(log, "late-explicit")
		return nil
	}}, SourcePlugin, "late", 200)

	if _, err := r.Run(context.Background(), ContextSnapshot, HookInput{}, AllowAll); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if log[0] != "late-explicit" {
		t.Fatalf("order override didn't pull 'late' first; log=%v", log)
	}
}
