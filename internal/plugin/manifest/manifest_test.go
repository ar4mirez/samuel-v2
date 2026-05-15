package manifest

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_Valid_Skill(t *testing.T) {
	body := []byte(`
name = "go-guide"
version = "1.4.2"
kind = "skill"
summary = "Go language guardrails"

[samuel]
framework = "^2.0.0"
protocol = "^1.0.0"

[provides]
skills = ["go-guide"]

[capabilities]
filesystem = { read = ["/workspace"], write = [] }
exec = false

[metadata]
language = "go"
extensions = [".go"]
auto_load = true
`)
	m, err := Parse(body, "test.toml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Name != "go-guide" || m.Kind != KindSkill {
		t.Errorf("manifest fields not set correctly: %+v", m)
	}
	if len(m.Provides.Skills) != 1 || m.Provides.Skills[0] != "go-guide" {
		t.Errorf("provides.skills: %v", m.Provides.Skills)
	}
	if len(m.Capabilities.Filesystem.Read) != 1 {
		t.Errorf("capabilities.filesystem.read: %v", m.Capabilities.Filesystem.Read)
	}
}

func TestParse_Valid_Wasm(t *testing.T) {
	body := []byte(`
name = "codex-translator"
version = "0.2.0"
kind = "wasm"

[wasm]
module = "plugin.wasm"
exports = ["translate", "lint", "health"]

[runtime]
max_memory = 64
timeout = "5s"
hard_timeout = "30s"
exports = ["translate", "lint"]
`)
	m, err := Parse(body, "wasm.toml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Wasm == nil || m.Wasm.Module != "plugin.wasm" {
		t.Errorf("wasm block not parsed: %+v", m.Wasm)
	}
	if m.Runtime == nil || m.Runtime.MaxMemoryMiB != 64 {
		t.Errorf("runtime block not parsed: %+v", m.Runtime)
	}
}

func TestParse_Wasm_RejectsReservedExport(t *testing.T) {
	body := []byte(`
name = "shadow"
version = "0.1.0"
kind = "wasm"

[wasm]
module = "plugin.wasm"
exports = ["init"]
`)
	_, err := Parse(body, "shadow.toml")
	if err == nil || !strings.Contains(err.Error(), "collides with built-in") {
		t.Fatalf("expected reserved-export rejection, got %v", err)
	}
}

func TestParse_Wasm_RequiresExports(t *testing.T) {
	body := []byte(`
name = "noexports"
version = "0.1.0"
kind = "wasm"

[wasm]
module = "plugin.wasm"
`)
	_, err := Parse(body, "noexports.toml")
	if err == nil || !strings.Contains(err.Error(), "no exports") {
		t.Fatalf("expected missing-exports rejection, got %v", err)
	}
}

func TestParse_Wasm_RequiresModule(t *testing.T) {
	body := []byte(`
name = "nomod"
version = "0.1.0"
kind = "wasm"

[runtime]
exports = ["lint"]
`)
	_, err := Parse(body, "nomod.toml")
	if err == nil || !strings.Contains(err.Error(), "module reference") {
		t.Fatalf("expected missing-module rejection, got %v", err)
	}
}

func TestParse_Valid_OCI(t *testing.T) {
	// PRD 0010 §Functional 2: image MUST be digest-pinned.
	digest := strings.Repeat("a", 64)
	body := []byte(`
name = "claude-runner"
version = "1.0.0"
kind = "oci"

[oci]
image = "ghcr.io/samuelpkg/samuel-runner-claude@sha256:` + digest + `"
`)
	m, err := Parse(body, "oci.toml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.OCI == nil || m.OCI.Image == "" {
		t.Errorf("oci block not parsed: %+v", m.OCI)
	}
}

// TestParse_OCI_RejectsTagOnly enforces PRD 0010 §Functional 2: an
// [oci].image without an @sha256 suffix must be rejected at parse time.
func TestParse_OCI_RejectsTagOnly(t *testing.T) {
	body := []byte(`
name = "claude-runner"
version = "1.0.0"
kind = "oci"

[oci]
image = "ghcr.io/samuelpkg/samuel-runner-claude:1.0.0"
`)
	_, err := Parse(body, "oci.toml")
	if err == nil || !strings.Contains(err.Error(), "digest-pinned") {
		t.Fatalf("expected digest-pinned rejection, got %v", err)
	}
}

func TestParse_RejectsBadKind(t *testing.T) {
	body := []byte(`
name = "xx"
version = "1.0.0"
kind = "unknown"
`)
	if _, err := Parse(body, "x.toml"); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("expected kind error, got %v", err)
	}
}

func TestParse_RejectsMissingName(t *testing.T) {
	body := []byte(`
version = "1.0.0"
kind = "skill"
`)
	_, err := Parse(body, "x.toml")
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected name error, got %v", err)
	}
}

func TestParse_RejectsInvalidName(t *testing.T) {
	body := []byte(`
name = "Bad_Name"
version = "1.0.0"
kind = "skill"
`)
	_, err := Parse(body, "x.toml")
	if err == nil {
		t.Fatalf("expected invalid name error")
	}
}

func TestParse_RejectsInvalidVersionRange(t *testing.T) {
	body := []byte(`
name = "xx"
version = "1.0.0"
kind = "skill"

[samuel]
framework = "garbage!"
`)
	_, err := Parse(body, "x.toml")
	if err == nil {
		t.Fatalf("expected invalid range error")
	}
}

func TestLoad_NotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
	if !errors.Is(err, err) {
		t.Fatalf("expected wrapping")
	}
}

func TestValidName(t *testing.T) {
	cases := map[string]bool{
		"go-guide":    true,
		"x":           false,
		"go":          true,
		"GoGuide":     false,
		"-leading":    false,
		"trailing-":   false,
		"a_b":         false,
		strings.Repeat("a", 64): true,
		strings.Repeat("a", 65): false,
	}
	for name, want := range cases {
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}
