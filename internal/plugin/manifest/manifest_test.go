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
exports = ["init", "run", "health"]
`)
	m, err := Parse(body, "wasm.toml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Wasm == nil || m.Wasm.Module != "plugin.wasm" {
		t.Errorf("wasm block not parsed: %+v", m.Wasm)
	}
}

func TestParse_Valid_OCI(t *testing.T) {
	body := []byte(`
name = "claude-runner"
version = "1.0.0"
kind = "oci"

[oci]
image = "ghcr.io/samuelpkg/samuel-runner-claude:1.0.0"
`)
	m, err := Parse(body, "oci.toml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.OCI == nil || m.OCI.Image == "" {
		t.Errorf("oci block not parsed: %+v", m.OCI)
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
