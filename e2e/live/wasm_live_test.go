//go:build e2e_live

package live

import (
	"os"
	"path/filepath"
	"testing"
)

// PRD 0009 §Functional 7: live e2e against the published reference
// plugin. These tests skip until `samuel-go-guide-wasm@1.0.0` is in
// the public registry — the framework lands ahead of the plugin
// release, and the registry publish step is owned by the plugin's
// own GitHub Actions release flow (examples/samuel-go-guide-wasm/
// .github/workflows/release.yml).
//
// Set SAMUEL_LIVE_WASM_PLUGIN=1 to enable. The framework's nightly
// e2e-live job exports the env once the plugin's first release tag
// has been observed by samuel-test-registry.

func wasmLiveEnabled() bool {
	return os.Getenv("SAMUEL_LIVE_WASM_PLUGIN") == "1"
}

func TestWASM_Live_InstallReference(t *testing.T) {
	if !wasmLiveEnabled() {
		t.Skip("samuel-go-guide-wasm not yet in live registry; set SAMUEL_LIVE_WASM_PLUGIN=1 to enable")
	}
	p := withLiveRegistry(t, nil)
	var out string
	if err := retryOnce(t, func() error {
		var execErr error
		out, execErr = p.samuel("install", "samuel-go-guide-wasm")
		return execErr
	}); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	assertContains(t, out, "Installed samuel-go-guide-wasm", "wasm install must succeed against the live registry")
	mod := filepath.Join(".samuel", "plugins", "samuel-go-guide-wasm", "plugin.wasm")
	if !p.fileExists(mod) {
		t.Errorf("plugin.wasm missing from installed plugin tree at %s", mod)
	}
}

func TestWASM_Live_InvokeReference(t *testing.T) {
	if !wasmLiveEnabled() {
		t.Skip("samuel-go-guide-wasm not yet in live registry; set SAMUEL_LIVE_WASM_PLUGIN=1 to enable")
	}
	p := withLiveRegistry(t, nil)
	if err := retryOnce(t, func() error {
		_, err := p.samuel("install", "samuel-go-guide-wasm")
		return err
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	out := p.mustSamuel("doctor")
	assertContains(t, out, "plugin:samuel-go-guide-wasm", "doctor must surface the live wasm plugin")
}
