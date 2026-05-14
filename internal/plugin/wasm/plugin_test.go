package wasm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/samuelpkg/samuel/internal/plugin"
	"github.com/samuelpkg/samuel/internal/plugin/capability"
	"github.com/samuelpkg/samuel/internal/plugin/manifest"
)

func writeFixture(t *testing.T, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.wasm"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWasm_InstallDetectCheck_Healthy(t *testing.T) {
	ctx := context.Background()
	rt, err := NewRuntime(ctx, "")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	src := writeFixture(t, BuildFixtureWasm(0 /*health*/, 1 /*protocol*/))
	project := t.TempDir()
	m := manifest.Manifest{
		Name: "codex-translator", Version: "0.1.0", Kind: manifest.KindWasm,
		Wasm: &manifest.WasmBlock{Module: "plugin.wasm"},
	}
	p := New(m, project, src, rt, nil)
	if _, err := p.Install(ctx, plugin.InstallOptions{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	det, _ := p.Detect(ctx)
	if !det.Installed {
		t.Errorf("expected installed")
	}
	st := p.Check(ctx)
	if !st.OK {
		t.Errorf("health should be OK, got %+v", st)
	}
}

func TestWasm_ProtocolRejection(t *testing.T) {
	ctx := context.Background()
	rt, err := NewRuntime(ctx, "")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)
	// Encode a fixture with protocol=99 — outside [1, 1] window.
	src := writeFixture(t, BuildFixtureWasm(0, 99))
	project := t.TempDir()
	m := manifest.Manifest{
		Name: "future", Version: "0.1.0", Kind: manifest.KindWasm,
		Wasm: &manifest.WasmBlock{Module: "plugin.wasm"},
	}
	p := New(m, project, src, rt, nil)
	if _, err := p.Install(ctx, plugin.InstallOptions{}); err == nil {
		t.Fatalf("Install should reject incompatible protocol")
	}
}

func TestWasm_HealthFailure(t *testing.T) {
	ctx := context.Background()
	rt, err := NewRuntime(ctx, "")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)
	// Encode a fixture with health=42 (non-zero = unhealthy), protocol=1.
	src := writeFixture(t, BuildFixtureWasm(42, 1))
	project := t.TempDir()
	m := manifest.Manifest{
		Name: "broken-health", Version: "0.1.0", Kind: manifest.KindWasm,
		Wasm: &manifest.WasmBlock{Module: "plugin.wasm"},
	}
	p := New(m, project, src, rt, nil)
	if _, err := p.Install(ctx, plugin.InstallOptions{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	st := p.Check(ctx)
	if st.OK {
		t.Errorf("Check should report not-ok for non-zero health, got %+v", st)
	}
}

func TestHostState_Authorize(t *testing.T) {
	state := &HostState{
		Plugin: "p",
		Grants: []capability.Grant{
			{Kind: capability.KindFilesystemRead, Targets: []string{"/workspace"}},
			{Kind: capability.KindNetworkOutbound, Targets: []string{"api.openai.com"}},
		},
	}
	if err := state.Authorize(capability.KindFilesystemRead, "/workspace/sub/file"); err != nil {
		t.Errorf("read inside workspace should be allowed: %v", err)
	}
	if err := state.Authorize(capability.KindFilesystemRead, "/etc/passwd"); err == nil {
		t.Errorf("read outside workspace must fail")
	}
	if err := state.Authorize(capability.KindNetworkOutbound, "api.openai.com:443"); err != nil {
		t.Errorf("outbound allowed host should pass: %v", err)
	}
	if err := state.Authorize(capability.KindNetworkOutbound, "evil.com:443"); err == nil {
		t.Errorf("outbound denied host must fail")
	}
}

func TestCacheKey_IsStable(t *testing.T) {
	body := BuildFixtureWasm(0, 1)
	k1 := CacheKey("p", "1.0.0", body)
	k2 := CacheKey("p", "1.0.0", body)
	if k1 != k2 {
		t.Errorf("cache key must be deterministic")
	}
}
