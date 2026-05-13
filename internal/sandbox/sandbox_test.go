package sandbox

import (
	"context"
	"strings"
	"testing"

	"github.com/ar4mirez/samuel/internal/agents"
)

func TestRun_HostMode_RunsRealBinary(t *testing.T) {
	r := New(t.TempDir())
	r.HostEnv = []string{"PATH=" + getPath()}
	res, err := r.Run(context.Background(), "echo", []string{"hello"}, agents.CommandOptions{
		EnvAllowlist: []string{"PATH"},
		Sandbox:      SandboxNone,
	})
	if err != nil {
		t.Fatalf("host echo: %v", err)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Fatalf("expected 'hello' in stdout; got %q", res.Stdout)
	}
}

func TestRun_HostMode_PropagatesExitCode(t *testing.T) {
	r := New(t.TempDir())
	r.HostEnv = []string{"PATH=" + getPath()}
	res, err := r.Run(context.Background(), "false", nil, agents.CommandOptions{
		EnvAllowlist: []string{"PATH"},
		Sandbox:      SandboxNone,
	})
	if err == nil {
		t.Fatal("expected error from false")
	}
	if res.ExitCode != 1 {
		t.Fatalf("expected exit code 1; got %d", res.ExitCode)
	}
}

func TestRun_DryRunMode_DoesNothing(t *testing.T) {
	r := New(t.TempDir())
	res, err := r.Run(context.Background(), "false", []string{"x"}, agents.CommandOptions{Sandbox: SandboxDryRun})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.HasPrefix(res.Stdout, "[sandbox-dry-run]") {
		t.Fatalf("expected dry-run marker; got %q", res.Stdout)
	}
}

func TestFilterEnv_OnlyAllowedVarsPassed(t *testing.T) {
	got := filterEnv([]string{"FOO=1", "BAR=2", "BAZ=3"}, []string{"FOO", "BAZ"})
	want := []string{"FOO=1", "BAZ=3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("filterEnv got %v want %v", got, want)
	}
}

func TestRun_OCIMode_RejectsMissingImage(t *testing.T) {
	r := New(t.TempDir())
	_, err := r.Run(context.Background(), "claude", []string{"-p", "x"}, agents.CommandOptions{
		Sandbox: SandboxOCI,
	})
	if err == nil || !strings.Contains(err.Error(), "requires an image") {
		t.Fatalf("expected image-required error; got %v", err)
	}
}

func TestRun_OCIMode_RejectsBadImageRef(t *testing.T) {
	r := New(t.TempDir())
	_, err := r.Run(context.Background(), "claude", nil, agents.CommandOptions{
		Sandbox:      SandboxOCI,
		SandboxImage: "BAD IMAGE NAME WITH SPACES",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func getPath() string {
	// Just enough PATH to find echo/false in tests; OS-default works.
	return "/usr/bin:/bin:/usr/sbin:/sbin"
}
