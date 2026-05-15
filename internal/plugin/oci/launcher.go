package oci

import (
	"context"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/samuelpkg/samuel/internal/plugin/capability"
	"github.com/samuelpkg/samuel/internal/plugin/manifest"
)

// LaunchOptions parameterizes a `<runtime> run` invocation. The
// orchestration that builds these from a plugin Manifest lives in the
// install path; the launcher itself is policy-free.
type LaunchOptions struct {
	Image  string
	Layout MountLayout
	// Capability grants the plugin received at install time. Drives
	// workspace writability + network policy.
	Grants []capability.Grant
	// EnvAllowlist names environment variables the framework may
	// forward into the container. Anything else is stripped.
	EnvAllowlist []string
	// HostEnv is the snapshot of the host environment the launcher
	// filters against EnvAllowlist. Tests pin this; production passes
	// os.Environ().
	HostEnv []string
	// Command overrides the image entrypoint when set.
	Command []string
	// Entrypoint overrides the image entrypoint (--entrypoint).
	// Multi-element entrypoints are joined into the leading position
	// of the command vector.
	Entrypoint []string
	// Workdir sets the container working directory (-w).
	Workdir string
	// CPUQuota maps to --cpus (PRD 0010 §Functional 3).
	CPUQuota string
	// MemoryLimit maps to --memory (PRD 0010 §Functional 3).
	MemoryLimit string
	// ExtraMounts are host-path → container-path bindings derived from
	// [capabilities.filesystem]. ReadOnly mirrors the manifest's
	// read/write split.
	ExtraMounts []CapabilityMount
	// NetworkPolicyMode forces the --network value, bypassing the
	// grant-driven default. Empty means "derive from grants". The
	// network-proxy path in PRD 0010 §Functional 5 sets this to "none"
	// so the proxy is the only egress.
	NetworkPolicyMode string
	// ProxySocket is the host path to the userspace network-policy
	// proxy socket. When non-empty it is bind-mounted into the
	// container at /samuel-proxy and the HTTP_PROXY/HTTPS_PROXY env
	// is injected (PRD 0010 §Functional 5.2).
	ProxySocket string
}

// CapabilityMount describes one extra -v bind derived from a manifest
// [capabilities.filesystem] entry.
type CapabilityMount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// CapabilityMountsFromManifest converts a manifest's
// [capabilities.filesystem] block into the launcher's CapabilityMount
// list. Read paths land as :ro mounts; write paths overlap by name —
// when the same path appears in both lists, write wins.
//
// Paths that are not already absolute container paths are interpreted
// relative to /workspace so plugin authors can write
// `write = ["data"]` instead of `["/workspace/data"]`. The first
// element of every absolute path becomes both host and container path
// (the framework owns the project tree → /workspace mapping).
func CapabilityMountsFromManifest(m *manifest.Manifest, projectDir string) []CapabilityMount {
	if m == nil {
		return nil
	}
	writes := map[string]struct{}{}
	for _, p := range m.Capabilities.Filesystem.Write {
		writes[normalizeContainerPath(p)] = struct{}{}
	}
	var out []CapabilityMount
	seen := map[string]struct{}{}
	add := func(p string, ro bool) {
		cp := normalizeContainerPath(p)
		if _, ok := seen[cp]; ok {
			return
		}
		seen[cp] = struct{}{}
		host := containerPathToHost(cp, projectDir)
		out = append(out, CapabilityMount{HostPath: host, ContainerPath: cp, ReadOnly: ro})
	}
	for _, p := range m.Capabilities.Filesystem.Read {
		cp := normalizeContainerPath(p)
		_, hasWrite := writes[cp]
		add(p, !hasWrite)
	}
	for _, p := range m.Capabilities.Filesystem.Write {
		add(p, false)
	}
	return out
}

// normalizeContainerPath promotes a glob/relative path to an absolute
// container path. /workspace is the canonical base for any non-absolute
// entry.
func normalizeContainerPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join("/workspace", p)
}

// containerPathToHost maps a container path back onto the host. The
// /workspace prefix is rewritten to projectDir; any other absolute path
// is bind-mounted into the container at the same location (used by
// plugin authors who explicitly opt in to mounting host directories).
func containerPathToHost(cp, projectDir string) string {
	switch {
	case cp == "/workspace":
		return projectDir
	case strings.HasPrefix(cp, "/workspace/"):
		return filepath.Join(projectDir, strings.TrimPrefix(cp, "/workspace/"))
	default:
		return cp
	}
}

// BuildRunArgs assembles the argument list for `<runtime> run`. The
// returned slice excludes the runtime binary itself.
//
// Layout:
//   --rm --user UID:GID
//   -v <Workspace>:/workspace[:ro]
//   -v <Skills>:/skills:ro
//   -v <SamuelRun>:/.samuel/run[:ro]
//   -v <PluginConfig>:/plugin/config:ro
//   -v <BridgeSocket>:/samuel-bridge
//   --network <policy>
//   -e KEY=VALUE  (one per allowed env var)
//   <image> [command...]
func BuildRunArgs(opts LaunchOptions) []string {
	args := []string{"run", "--rm"}
	if uid, gid := currentUIDGID(); uid != "" {
		args = append(args, "--user", uid+":"+gid)
	}
	workspaceMode := ":ro"
	if hasWriteCapability(opts.Grants) {
		workspaceMode = ""
	}
	samuelRunMode := workspaceMode
	if opts.Layout.Workspace != "" {
		args = append(args, "-v", opts.Layout.Workspace+":/workspace"+workspaceMode)
	}
	if opts.Layout.Skills != "" {
		args = append(args, "-v", opts.Layout.Skills+":/skills:ro")
	}
	if opts.Layout.SamuelRun != "" {
		args = append(args, "-v", opts.Layout.SamuelRun+":/.samuel/run"+samuelRunMode)
	}
	if opts.Layout.PluginConfig != "" {
		args = append(args, "-v", opts.Layout.PluginConfig+":/plugin/config:ro")
	}
	if opts.Layout.BridgeSocket != "" {
		args = append(args, "-v", opts.Layout.BridgeSocket+":/samuel-bridge")
	}
	// Manifest-declared filesystem mounts (PRD 0010 §Functional 3.1).
	for _, m := range opts.ExtraMounts {
		mode := ""
		if m.ReadOnly {
			mode = ":ro"
		}
		args = append(args, "-v", m.HostPath+":"+m.ContainerPath+mode)
	}
	// Network proxy mount + env injection (PRD 0010 §Functional 5.2).
	if opts.ProxySocket != "" {
		args = append(args, "-v", opts.ProxySocket+":/samuel-proxy")
		args = append(args, "-e", "HTTP_PROXY=unix:///samuel-proxy")
		args = append(args, "-e", "HTTPS_PROXY=unix:///samuel-proxy")
		args = append(args, "-e", "ALL_PROXY=unix:///samuel-proxy")
	}
	// Network policy: explicit override > derived from grants.
	network := opts.NetworkPolicyMode
	if network == "" {
		network = networkPolicy(opts.Grants)
	}
	args = append(args, "--network", network)

	// Resource limits (PRD 0010 §Functional 3.3).
	if opts.CPUQuota != "" {
		args = append(args, "--cpus", opts.CPUQuota)
	}
	if opts.MemoryLimit != "" {
		args = append(args, "--memory", opts.MemoryLimit)
	}

	// Working directory + entrypoint override.
	if opts.Workdir != "" {
		args = append(args, "-w", opts.Workdir)
	}
	if len(opts.Entrypoint) > 0 {
		args = append(args, "--entrypoint", opts.Entrypoint[0])
	}

	for _, kv := range filterEnv(opts.HostEnv, opts.EnvAllowlist) {
		args = append(args, "-e", kv)
	}

	args = append(args, opts.Image)
	// When the manifest declares a multi-element entrypoint, the extra
	// args follow the image (Docker convention).
	if len(opts.Entrypoint) > 1 {
		args = append(args, opts.Entrypoint[1:]...)
	}
	args = append(args, opts.Command...)
	return args
}

// Launch shells out to the runtime CLI with the prepared run args. The
// process is started in the background; the bridge handles all
// further communication. Callers can context.WithCancel to terminate.
func Launch(ctx context.Context, runtime DetectedRuntime, opts LaunchOptions) (*exec.Cmd, error) {
	args := BuildRunArgs(opts)
	cmd := exec.CommandContext(ctx, runtime.Path, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("oci: launch %s: %w", opts.Image, err)
	}
	return cmd, nil
}

// hasWriteCapability returns true when the grants contain any
// filesystem.write entry pointing at /workspace.
func hasWriteCapability(grants []capability.Grant) bool {
	for _, g := range grants {
		if g.Kind != capability.KindFilesystemWrite {
			continue
		}
		for _, t := range g.Targets {
			if t == "/workspace" || strings.HasPrefix(t, "/workspace/") {
				return true
			}
		}
	}
	return false
}

// networkPolicy maps the network.outbound capability to a runtime
// network flag value. The PRD calls for deny-by-default; only when the
// plugin explicitly requests outbound do we pass through `bridge`.
//
// The runtime-level allowlist (per-destination filter) is enforced by
// `--add-host` rules + iptables in production; v2.0 ships the binary
// gate (none / bridge) and the bridge.MatchHost host-function check
// handles per-call filtering.
func networkPolicy(grants []capability.Grant) string {
	for _, g := range grants {
		if g.Kind == capability.KindNetworkOutbound && len(g.Targets) > 0 {
			return "bridge"
		}
	}
	return "none"
}

// filterEnv returns key=value pairs from env where the key is in allow.
func filterEnv(env, allow []string) []string {
	if len(allow) == 0 {
		return nil
	}
	allowSet := make(map[string]struct{}, len(allow))
	for _, a := range allow {
		allowSet[a] = struct{}{}
	}
	var out []string
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

func currentUIDGID() (string, string) {
	u, err := user.Current()
	if err != nil {
		return "", ""
	}
	return u.Uid, u.Gid
}
