package wasm

import (
	"strings"
	"testing"
	"time"

	"github.com/samuelpkg/samuel/internal/plugin/manifest"
)

func TestCapabilities_DefaultsApplied(t *testing.T) {
	m := &manifest.Manifest{
		Kind: manifest.KindWasm,
		Wasm: &manifest.WasmBlock{Module: "plugin.wasm", Exports: []string{"lint"}},
	}
	c, err := CapabilitiesFromManifest(m)
	if err != nil {
		t.Fatalf("CapabilitiesFromManifest: %v", err)
	}
	if c.MaxMemoryMiB != DefaultMaxMemoryMiB {
		t.Errorf("default memory not applied: got %d, want %d", c.MaxMemoryMiB, DefaultMaxMemoryMiB)
	}
	if c.SoftTimeout != DefaultSoftTimeout {
		t.Errorf("default soft timeout not applied: got %v", c.SoftTimeout)
	}
	if c.HardTimeout != DefaultHardTimeout {
		t.Errorf("default hard timeout not applied: got %v", c.HardTimeout)
	}
}

func TestCapabilities_FromManifest_RoundTrips(t *testing.T) {
	m := &manifest.Manifest{
		Kind: manifest.KindWasm,
		Wasm: &manifest.WasmBlock{Module: "plugin.wasm", Exports: []string{"lint"}},
		Capabilities: manifest.CapabilitiesBlock{
			Filesystem: manifest.FilesystemCaps{
				Read:  []string{"/workspace"},
				Write: []string{"/tmp/out"},
			},
			Network: manifest.NetworkCaps{Hosts: []string{"api.example.com"}},
			Env:     []string{"HOME", "PATH"},
		},
		Runtime: &manifest.RuntimeBlock{
			MaxMemoryMiB: 32,
			Timeout:      "2s",
			HardTimeout:  "10s",
			Exports:      []string{"lint"},
		},
	}
	c, err := CapabilitiesFromManifest(m)
	if err != nil {
		t.Fatalf("CapabilitiesFromManifest: %v", err)
	}
	if c.MaxMemoryMiB != 32 || c.SoftTimeout != 2*time.Second || c.HardTimeout != 10*time.Second {
		t.Errorf("budgets not applied: %+v", c)
	}
	if len(c.Filesystem) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(c.Filesystem))
	}
	if !c.Filesystem[0].ReadOnly {
		t.Errorf("first mount should be read-only")
	}
	if c.Filesystem[1].ReadOnly {
		t.Errorf("second mount should be writable")
	}
	if !c.AllowsHost("api.example.com") || c.AllowsHost("evil.com") {
		t.Errorf("network allowlist mismatch")
	}
}

func TestCapabilities_FilesystemEscape_Denied(t *testing.T) {
	c := Capabilities{}.withFilesystem("/workspace", true)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if !c.AllowsPath("/workspace/sub/file.go", false) {
		t.Errorf("read inside /workspace should be allowed")
	}
	if c.AllowsPath("/etc/passwd", false) {
		t.Errorf("read outside /workspace must be denied")
	}
	if c.AllowsPath("/workspace/sub/file.go", true) {
		t.Errorf("write to read-only mount must be denied")
	}
}

func TestCapabilities_Conflict_WriteWithoutMount(t *testing.T) {
	// A write-only mount with no read mount is valid; a relative path
	// is not.
	c := Capabilities{Filesystem: []FilesystemMount{{HostPath: "relative/path"}}}
	if err := c.Validate(); err == nil {
		t.Fatalf("expected validation to reject relative mount")
	}
}

func TestCapabilities_Env_EmptyMeansNone(t *testing.T) {
	m := &manifest.Manifest{
		Kind:         manifest.KindWasm,
		Wasm:         &manifest.WasmBlock{Module: "plugin.wasm", Exports: []string{"lint"}},
		Capabilities: manifest.CapabilitiesBlock{},
	}
	c, err := CapabilitiesFromManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Env) != 0 {
		t.Errorf("empty env list expected, got %v", c.Env)
	}
}

func TestCapabilities_Network_DenyByDefault(t *testing.T) {
	m := &manifest.Manifest{
		Kind: manifest.KindWasm,
		Wasm: &manifest.WasmBlock{Module: "plugin.wasm", Exports: []string{"lint"}},
	}
	c, err := CapabilitiesFromManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if c.AllowsHost("anything.com") {
		t.Errorf("network must deny by default when no [capabilities.network] block declared")
	}
}

func TestCapabilities_WithEnv_AppendsAndDedups(t *testing.T) {
	c := Capabilities{}.withEnv("HOME", "PATH").withEnv("HOME", "USER")
	got := map[string]bool{}
	for _, k := range c.Env {
		got[k] = true
	}
	for _, want := range []string{"HOME", "PATH", "USER"} {
		if !got[want] {
			t.Errorf("withEnv should retain %q, got %v", want, c.Env)
		}
	}
	if len(c.Env) != 3 {
		t.Errorf("expected 3 unique keys after dedup, got %d (%v)", len(c.Env), c.Env)
	}
}

func TestCapabilities_Network_WildcardSubdomain(t *testing.T) {
	c := Capabilities{}.withNetwork("*.example.com")
	if !c.AllowsHost("api.example.com") {
		t.Errorf("*.example.com should match api.example.com")
	}
	if !c.AllowsHost("nested.api.example.com") {
		t.Errorf("*.example.com should match nested.api.example.com")
	}
	if c.AllowsHost("evil.com") {
		t.Errorf("*.example.com must not match evil.com")
	}
}

func TestCapabilities_TimeoutValidation(t *testing.T) {
	c := Capabilities{SoftTimeout: 30 * time.Second, HardTimeout: 5 * time.Second}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "soft timeout") {
		t.Fatalf("expected soft>hard rejection, got %v", err)
	}
}
