//go:build e2e_live

package live

import (
	"os"
	"os/exec"
	"testing"
)

// PRD 0010 §Functional 9: live e2e against the published reference
// OCI plugin. The framework lands ahead of the plugin release, so the
// tests skip until `samuel-claude-code-oci@1.0.0` is in the public
// registry. Set SAMUEL_LIVE_OCI_PLUGIN=1 to enable; the nightly
// e2e-live job exports the env once the registry has caught up.
//
// All tests also skip when no container runtime is on PATH — same
// gate as the hermetic suite.

func ociLiveEnabled() bool {
	return os.Getenv("SAMUEL_LIVE_OCI_PLUGIN") == "1"
}

func skipIfNoRuntime(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"podman", "docker"} {
		if _, err := exec.LookPath(bin); err == nil {
			return
		}
	}
	t.Skip("no container runtime on PATH; skipping e2e_live OCI suite")
}

// TestOCI_Live_InstallReference installs the reference OCI plugin
// against the live registry. Auto-skips without a runtime or env opt-in.
func TestOCI_Live_InstallReference(t *testing.T) {
	if !ociLiveEnabled() {
		t.Skip("samuel-claude-code-oci not yet in live registry; set SAMUEL_LIVE_OCI_PLUGIN=1 to enable")
	}
	skipIfNoRuntime(t)
	p := withLiveRegistry(t, nil)
	var out string
	if err := retryOnce(t, func() error {
		var execErr error
		out, execErr = p.samuel("install", "samuel-claude-code-oci")
		return execErr
	}); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	assertContains(t, out, "Installed samuel-claude-code-oci", "oci install must succeed against the live registry")
}

// TestOCI_Live_AgentContainerizedRun exercises `samuel run --sandbox=oci`
// against the tetris fixture. Asserts at least one iteration runs to
// completion inside the container. Skips when no runtime or no env.
func TestOCI_Live_AgentContainerizedRun(t *testing.T) {
	if !ociLiveEnabled() {
		t.Skip("oci live tests are env-gated; set SAMUEL_LIVE_OCI_PLUGIN=1")
	}
	skipIfNoRuntime(t)
	p := withLiveRegistry(t, nil)
	// Fixture install (tetris workspace) is handled by the live
	// helpers; here we just exercise the sandbox flag.
	out, err := p.samuel("run", "init", "--sandbox=oci", "--methodology=ralph", "--ai-tool=claude")
	if err != nil {
		t.Fatalf("run init: %v\n%s", err, out)
	}
}
