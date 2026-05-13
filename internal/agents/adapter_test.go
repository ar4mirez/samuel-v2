package agents

import (
	"context"
	"strings"
	"testing"
)

type mockRunner struct {
	got      []string
	gotStdin string
	gotOpts  CommandOptions
	out      Result
	err      error
}

func (m *mockRunner) Run(_ context.Context, _ string, args []string, opts CommandOptions) (Result, error) {
	m.got = args
	m.gotStdin = opts.Stdin
	m.gotOpts = opts
	return m.out, m.err
}

func TestBuiltins_AllRegistered(t *testing.T) {
	for _, name := range []string{"claude", "codex", "copilot", "gemini", "kiro"} {
		if _, ok := Get(name); !ok {
			t.Fatalf("adapter %q not registered", name)
		}
	}
}

func TestClaudeAdapter_BuildsContentArg(t *testing.T) {
	a, _ := Get("claude")
	args := a.BuildArgs(Options{PromptContent: "do the thing"})
	if args[0] != "-p" || args[1] != "do the thing" {
		t.Fatalf("unexpected args: %v", args)
	}
	if !strings.Contains(strings.Join(args, " "), "--dangerously-skip-permissions") {
		t.Fatalf("missing permission flag: %v", args)
	}
}

func TestCodexAdapter_BuildsBypassExec(t *testing.T) {
	a, _ := Get("codex")
	args := a.BuildArgs(Options{PromptContent: "task"})
	if args[0] != "--dangerously-bypass-approvals-and-sandbox" || args[1] != "exec" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCopilotAdapter_UsesFileArg(t *testing.T) {
	a, _ := Get("copilot")
	args := a.BuildArgs(Options{PromptPath: "/run/prompt.md"})
	if args[0] != "--prompt" || args[1] != "/run/prompt.md" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestKiroAdapter_PromptOnStdin(t *testing.T) {
	a, _ := Get("kiro")
	m := &mockRunner{}
	res, err := a.Invoke(context.Background(), Options{
		PromptContent: "hello",
		CommandRunner: m,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	_ = res
	if m.gotStdin != "hello" {
		t.Fatalf("expected stdin=hello, got %q", m.gotStdin)
	}
}

func TestDryRun_SkipsRunner(t *testing.T) {
	a, _ := Get("claude")
	m := &mockRunner{}
	res, err := a.Invoke(context.Background(), Options{
		PromptContent: "x",
		DryRun:        true,
		CommandRunner: m,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.HasPrefix(res.Stdout, "[dry-run]") {
		t.Fatalf("expected dry-run marker; got %q", res.Stdout)
	}
	if m.got != nil {
		t.Fatalf("runner should not be invoked under DryRun; got args=%v", m.got)
	}
}

func TestSandboxImage_FallsBackToDefault(t *testing.T) {
	a, _ := Get("claude")
	m := &mockRunner{}
	_, _ = a.Invoke(context.Background(), Options{PromptContent: "x", CommandRunner: m})
	if m.gotOpts.SandboxImage == "" {
		t.Fatal("expected default image to be filled in")
	}
}

func TestRegister_LastWins(t *testing.T) {
	const name = "custom-test"
	a1 := &builtinAdapter{manifest: Manifest{Name: name}}
	a2 := &builtinAdapter{manifest: Manifest{Name: name, DefaultImage: "newer"}}
	Register(a1)
	Register(a2)
	got, _ := Get(name)
	if got.Manifest().DefaultImage != "newer" {
		t.Fatal("Register should be last-wins")
	}
}
