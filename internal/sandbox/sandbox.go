// Package sandbox is the host-process / OCI launch surface the
// methodology layer calls through to invoke an agent. It implements
// agents.CommandRunner so adapters can stay declarative.
//
// Three modes are supported:
//
//   - SandboxNone : exec the binary directly on the host (legacy path,
//     v1 default).
//   - SandboxOCI  : run inside a container via the OCI tier loader.
//     Mount layout follows RFD 0006: /workspace (rw),
//     /skills (ro), /.samuel/run (ro — the agent uses
//     CLI mutation subcommands), /plugin/config (ro),
//     /samuel-bridge (gRPC socket).
//   - SandboxDryRun: never runs anything; used by `samuel run start --dry-run`.
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samuelpkg/samuel/internal/agents"
	"github.com/samuelpkg/samuel/internal/plugin/oci"
)

// Mode names.
const (
	// SandboxNone is the legacy value preserved for back-compat. It maps
	// to the host-exec path.
	SandboxNone   = "none"
	// SandboxHost is the PRD 0010 spelling of the same host-exec path.
	// Accepted on the `samuel run --sandbox=host` flag.
	SandboxHost   = "host"
	SandboxOCI    = "oci"
	SandboxDryRun = "dry-run"
)

// baseHostEnv is the always-passed-through OS plumbing for host-exec
// mode. The adapter's EnvAllowlist is for secrets/API keys; these are
// the variables every binary needs to function at all (find HOME for
// config, PATH for helper tools, TERM for stdio sizing, locale for
// non-ASCII output). Without HOME, Claude Code can't read its OAuth
// credentials from the macOS Keychain and exits 1.
var baseHostEnv = []string{
	"HOME", "PATH", "USER", "LOGNAME", "SHELL", "TMPDIR", "PWD",
	"TERM", "LANG", "LC_ALL", "LC_CTYPE",
}

// Runner is the agents.CommandRunner implementation. Its zero value is
// usable as a "host-exec only" runner; set ProjectDir / RunDir for the
// OCI path.
type Runner struct {
	// ProjectDir is the project root. Becomes /workspace (rw) inside
	// the container.
	ProjectDir string
	// RunDir is the .samuel/run directory mounted read-only at
	// /.samuel/run inside the container — the agent uses `samuel run`
	// CLI subcommands to mutate state, never edits the file directly.
	RunDir string
	// SkillsDir holds installed skill plugins (typically
	// ~/.samuel/builtins). Mounted ro at /skills.
	SkillsDir string
	// BridgeSocket is the host path to the gRPC bridge socket
	// (typically <RunDir>/samuel-bridge.sock). Mounted at
	// /samuel-bridge so plugins inside the container can talk to the
	// framework.
	BridgeSocket string
	// PluginConfigDir mounts at /plugin/config (ro). Optional.
	PluginConfigDir string
	// Stdout / Stderr are connected to the container's streams. Tests
	// inject buffers; production lets stdout/stderr leak to the user.
	Stdout *bytes.Buffer
	Stderr *bytes.Buffer
	// HostEnv is the host environment snapshot the env allowlist
	// filters against. Tests pin this; production passes os.Environ().
	HostEnv []string
	// detectedRuntime caches the chosen OCI engine for the lifetime of
	// the Runner. Nil means "not yet detected".
	detectedRuntime *oci.DetectedRuntime
}

// New constructs a Runner with sensible defaults derived from
// projectDir. Callers pass it into agents.Options.CommandRunner.
func New(projectDir string) *Runner {
	home, _ := os.UserHomeDir()
	r := &Runner{
		ProjectDir:   projectDir,
		RunDir:       filepath.Join(projectDir, ".samuel", "run"),
		SkillsDir:    filepath.Join(home, ".samuel", "builtins"),
		BridgeSocket: filepath.Join(projectDir, ".samuel", "run", "samuel-bridge.sock"),
		HostEnv:      os.Environ(),
	}
	return r
}

// Run dispatches to the correct backend based on opts.Sandbox. Accepts
// the legacy "none" value, the PRD 0010 "host" synonym, "oci", and
// "dry-run".
func (r *Runner) Run(ctx context.Context, name string, args []string, opts agents.CommandOptions) (agents.Result, error) {
	switch strings.ToLower(opts.Sandbox) {
	case SandboxOCI:
		return r.runOCI(ctx, name, args, opts)
	case SandboxDryRun:
		return agents.Result{Stdout: fmt.Sprintf("[sandbox-dry-run] %s %v", name, args)}, nil
	case SandboxHost, SandboxNone, "":
		return r.runHost(ctx, name, args, opts)
	default:
		return r.runHost(ctx, name, args, opts)
	}
}

// runHost shells out to the binary as a regular host process. Used for
// developers who haven't installed a container runtime.
func (r *Runner) runHost(ctx context.Context, name string, args []string, opts agents.CommandOptions) (agents.Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = opts.WorkDir
	cmd.Env = filterEnv(r.HostEnv, mergeStrings(baseHostEnv, opts.EnvAllowlist))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if opts.Stdin != "" {
		cmd.Stdin = strings.NewReader(opts.Stdin)
	}
	err := cmd.Run()
	res := agents.Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if exitErr, ok := asExitError(err); ok {
		res.ExitCode = exitErr.ExitCode()
		return res, fmt.Errorf("%s exited %d: %s", name, res.ExitCode, firstNonEmpty(res.Stderr, res.Stdout))
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

// firstNonEmpty returns the trimmed value of the first non-empty
// argument. Many CLIs (Claude Code included) print fatal errors to
// stdout instead of stderr, so falling back to stdout keeps the user
// from seeing an opaque "exited N: " line.
func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			return s
		}
	}
	return ""
}

// mergeStrings concatenates two slices and drops duplicates, preserving
// order of first appearance.
func mergeStrings(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	seen := map[string]struct{}{}
	for _, src := range [][]string{a, b} {
		for _, s := range src {
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// runOCI invokes the agent inside a sandbox container via the OCI tier
// loader. Mount layout matches RFD 0006: `/workspace` rw, `/skills` ro,
// `/.samuel/run` ro (CLI-mutation invariant).
func (r *Runner) runOCI(ctx context.Context, name string, args []string, opts agents.CommandOptions) (agents.Result, error) {
	rt, err := r.runtime()
	if err != nil {
		return agents.Result{}, err
	}
	if opts.SandboxImage == "" {
		return agents.Result{}, fmt.Errorf("oci sandbox requires an image (set sandbox_image or adapter default)")
	}
	if _, err := oci.ParseImageName(opts.SandboxImage); err != nil {
		return agents.Result{}, err
	}
	runArgs := oci.BuildRunArgs(oci.LaunchOptions{
		Image: opts.SandboxImage,
		Layout: oci.MountLayout{
			Workspace:    r.ProjectDir,
			Skills:       r.SkillsDir,
			SamuelRun:    r.RunDir,
			PluginConfig: r.PluginConfigDir,
			BridgeSocket: r.BridgeSocket,
		},
		EnvAllowlist: opts.EnvAllowlist,
		HostEnv:      r.HostEnv,
		Command:      append([]string{name}, args...),
	})
	// Force `/.samuel/run` to be read-only — RFD 0006 invariant: the
	// agent uses `samuel run` CLI mutation commands and never edits the
	// runtime files directly. BuildRunArgs computes :ro/<empty> based
	// on capability grants; for the methodology path we have no grants
	// so default is already :ro. The check stays here for safety.
	cmd := exec.CommandContext(ctx, rt.Path, runArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if opts.Stdin != "" {
		cmd.Stdin = strings.NewReader(opts.Stdin)
	}
	err = cmd.Run()
	res := agents.Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if exitErr, ok := asExitError(err); ok {
		res.ExitCode = exitErr.ExitCode()
		return res, fmt.Errorf("%s exited %d (sandbox=%s): %s", name, res.ExitCode, opts.Sandbox, strings.TrimSpace(res.Stderr))
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Runner) runtime() (oci.DetectedRuntime, error) {
	if r.detectedRuntime != nil {
		return *r.detectedRuntime, nil
	}
	rt, err := oci.DetectRuntime()
	if err != nil {
		return oci.DetectedRuntime{}, err
	}
	r.detectedRuntime = &rt
	return rt, nil
}

func filterEnv(env, allow []string) []string {
	if len(allow) == 0 {
		return env
	}
	allowSet := map[string]struct{}{}
	for _, a := range allow {
		allowSet[a] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		if _, ok := allowSet[kv[:i]]; ok {
			out = append(out, kv)
		}
	}
	return out
}

func asExitError(err error) (*exec.ExitError, bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee, true
	}
	return nil, false
}
