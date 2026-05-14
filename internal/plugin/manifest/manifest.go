// Package manifest parses, validates, and renders samuel-plugin.toml —
// the on-disk descriptor every installable Samuel plugin ships at the
// root of its repository (or inside its OCI image).
//
// The full schema is defined in RFD 0003. This package covers the
// subset PRD 0003 needs:
//
//   - name, version, kind ("skill" | "wasm" | "oci")
//   - [samuel] framework + protocol version ranges
//   - [provides] skills / commands / methodology / hooks
//   - [requires] inter-plugin deps
//   - [capabilities] filesystem.read/write, exec, network.outbound
//   - [metadata] free-form key/value
//   - [wasm] module + exports (when kind = "wasm")
//   - [oci] image (digest pinned at install time)
//
// Validation errors are returned as *errors.Error with a stable docs URL
// so users can click straight to the fix. The package is the source of
// truth for "is this manifest well-formed?"; downstream loaders trust the
// shape after a successful Load.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/samuelpkg/samuel/internal/errors"
)

// FileName is the canonical on-disk filename for a plugin manifest.
const FileName = "samuel-plugin.toml"

// Component is the namespace used in structured errors emitted by this
// package.
const Component = "plugin/manifest"

// Kind enumerates the plugin tiers PRD 0003 wires up.
type Kind string

const (
	KindSkill Kind = "skill"
	KindWasm  Kind = "wasm"
	KindOci   Kind = "oci"
	// KindMeta is a payload-free plugin that exists solely to declare a
	// [requires] graph. The loader resolves the deps and never copies
	// content for the meta itself. Used by samuel-starter to bootstrap
	// the Samuel Way workflow plugins on `samuel init`.
	KindMeta Kind = "meta"
)

// Manifest is the parsed samuel-plugin.toml. Fields that are optional in
// the schema use pointer or zero-value semantics; required fields are
// validated by Validate.
type Manifest struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
	Kind    Kind   `toml:"kind"`

	Samuel       SamuelBlock       `toml:"samuel,omitempty"`
	Provides     ProvidesBlock     `toml:"provides,omitempty"`
	Requires     map[string]string `toml:"requires,omitempty"`
	Capabilities CapabilitiesBlock `toml:"capabilities,omitempty"`
	Metadata     map[string]any    `toml:"metadata,omitempty"`

	Wasm *WasmBlock `toml:"wasm,omitempty"`
	OCI  *OCIBlock  `toml:"oci,omitempty"`

	// Runtime carries wasm-tier memory + timeout budgets (PRD 0009).
	// Skill and OCI tiers ignore the block.
	Runtime *RuntimeBlock `toml:"runtime,omitempty"`

	// Summary, Homepage, License, Authors mirror the RFD 0003 manifest
	// schema. They are optional and surface through `samuel info`.
	Summary  string   `toml:"summary,omitempty"`
	Homepage string   `toml:"homepage,omitempty"`
	License  string   `toml:"license,omitempty"`
	Authors  []string `toml:"authors,omitempty"`
}

// SamuelBlock pins compatibility ranges. `framework` is the samuel CLI
// version, `protocol` is the plugin-protocol version (kept separate so
// the protocol can evolve independently per RFD 0001 resolution #2).
type SamuelBlock struct {
	Framework string `toml:"framework,omitempty"`
	Protocol  string `toml:"protocol,omitempty"`
}

// ProvidesBlock lists the artifacts a plugin contributes — skills,
// commands, methodologies, or hooks. Empty lists are valid.
type ProvidesBlock struct {
	Skills      []string `toml:"skills,omitempty"`
	Commands    []string `toml:"commands,omitempty"`
	Methodology []string `toml:"methodology,omitempty"`
	Hooks       []string `toml:"hooks,omitempty"`
}

// CapabilitiesBlock declares the host resources the plugin needs at
// runtime. The capability model is enforced at install time
// (`samuel install` prompts) and at runtime (per-tier loader gates the
// host functions / mounts / network policy).
type CapabilitiesBlock struct {
	Filesystem FilesystemCaps `toml:"filesystem,omitempty"`
	Exec       bool           `toml:"exec,omitempty"`
	Network    NetworkCaps    `toml:"network,omitempty"`
	// Env is the per-process env-key allowlist enforced by the wasm
	// runtime (PRD 0009 §Functional 1). Empty list = no env.
	Env []string `toml:"env,omitempty"`
	// Samuel namespace covers framework-internal capabilities
	// (samuel.api access, assistant.invoke, etc.).
	Samuel    SamuelCaps    `toml:"samuel,omitempty"`
	Assistant AssistantCaps `toml:"assistant,omitempty"`
}

// FilesystemCaps lists path-glob allowlists for read/write filesystem
// access. Globs are evaluated by bmatcuk/doublestar at host-function
// invocation time (skill tier reads only; wasm + oci tiers gate at the
// runtime boundary).
type FilesystemCaps struct {
	Read  []string `toml:"read,omitempty"`
	Write []string `toml:"write,omitempty"`
}

// NetworkCaps lists outbound destination allowlists. `outbound` is the
// legacy v2.0 field used by skill + oci tiers; `hosts` is the wasm-tier
// allowlist enforced by the wasiNetwork proxy (PRD 0009).
type NetworkCaps struct {
	Outbound []string `toml:"outbound,omitempty"`
	Hosts    []string `toml:"hosts,omitempty"`
}

// RuntimeBlock carries wasm-tier per-instance budgets (PRD 0009).
//
//	[runtime]
//	  max_memory  = 64        # MiB; wazero memory cap
//	  timeout     = "5s"      # soft deadline (returned to plugin)
//	  hard_timeout = "30s"    # absolute kill via ctx.Cancel
//	  exports     = ["lint"]  # functions installer is allowed to call
type RuntimeBlock struct {
	MaxMemoryMiB uint32   `toml:"max_memory,omitempty"`
	Timeout      string   `toml:"timeout,omitempty"`
	HardTimeout  string   `toml:"hard_timeout,omitempty"`
	Exports      []string `toml:"exports,omitempty"`
}

// SamuelCaps gates access to framework-internal RPC surfaces.
type SamuelCaps struct {
	API bool `toml:"api,omitempty"`
}

// AssistantCaps gates invocation of the user's coding assistant
// (claude/codex/gemini/etc.) from inside a plugin.
type AssistantCaps struct {
	Invoke bool `toml:"invoke,omitempty"`
}

// WasmBlock holds wasm-tier-specific fields.
type WasmBlock struct {
	Module  string   `toml:"module"`
	Exports []string `toml:"exports,omitempty"`
}

// OCIBlock holds oci-tier-specific fields. Digest is pinned at install
// time and recorded in samuel.lock — the manifest only carries the
// floating image reference.
type OCIBlock struct {
	Image  string `toml:"image"`
	Digest string `toml:"digest,omitempty"`
}

// Load reads and parses the manifest at path. Validation runs on the
// parsed value before returning.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, (&errors.Error{
			Component:   Component,
			Problem:     "cannot read manifest",
			Path:        path,
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
			Recoverable: true,
		}).Wrap(err)
	}
	return Parse(data, path)
}

// LoadFromDir reads <dir>/samuel-plugin.toml.
func LoadFromDir(dir string) (*Manifest, error) {
	return Load(filepath.Join(dir, FileName))
}

// Parse decodes raw TOML bytes; pathHint is only used for error
// reporting (the user-visible filename).
func Parse(data []byte, pathHint string) (*Manifest, error) {
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, (&errors.Error{
			Component:   Component,
			Problem:     "manifest is not valid TOML",
			Path:        pathHint,
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
			Recoverable: true,
		}).Wrap(err)
	}
	if err := m.Validate(); err != nil {
		if e, ok := err.(*errors.Error); ok && e.Path == "" {
			e.Path = pathHint
		}
		return nil, err
	}
	return &m, nil
}

// Validate runs structural checks on a parsed manifest. Returns nil iff
// every required field is set and every constrained field carries a
// well-formed value.
func (m *Manifest) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return &errors.Error{
			Component:   Component,
			Problem:     "manifest missing required field 'name'",
			Fix:         "add `name = \"<plugin-name>\"` at the top of samuel-plugin.toml",
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
			Recoverable: true,
		}
	}
	if !ValidName(m.Name) {
		return &errors.Error{
			Component:   Component,
			Problem:     fmt.Sprintf("invalid plugin name %q", m.Name),
			Fix:         "names must match [a-z0-9][a-z0-9-]* and be 2-64 chars",
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
			Recoverable: true,
		}
	}
	if strings.TrimSpace(m.Version) == "" {
		return &errors.Error{
			Component:   Component,
			Problem:     "manifest missing required field 'version'",
			Fix:         "add `version = \"X.Y.Z\"` matching the latest tagged release",
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
			Recoverable: true,
		}
	}
	switch m.Kind {
	case KindSkill, KindWasm, KindOci, KindMeta:
	case "":
		return &errors.Error{
			Component:   Component,
			Problem:     "manifest missing required field 'kind'",
			Fix:         "set `kind = \"skill\" | \"wasm\" | \"oci\" | \"meta\"`",
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
			Recoverable: true,
		}
	default:
		return &errors.Error{
			Component:   Component,
			Problem:     fmt.Sprintf("invalid plugin kind %q", m.Kind),
			Fix:         "kind must be one of: skill, wasm, oci, meta",
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
			Recoverable: true,
		}
	}

	if m.Kind == KindMeta && len(m.Requires) == 0 {
		return &errors.Error{
			Component:   Component,
			Problem:     "meta plugin must declare at least one entry in [requires]",
			Fix:         "add `[requires]\\n<plugin-name> = \"^X.Y.Z\"`",
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
			Recoverable: true,
		}
	}

	if m.Kind == KindWasm {
		if m.Wasm == nil || strings.TrimSpace(m.Wasm.Module) == "" {
			return &errors.Error{
				Component:   Component,
				Problem:     "wasm manifest missing [wasm] module reference",
				Fix:         "add `[wasm] module = \"plugin.wasm\"`",
				DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
				Recoverable: true,
			}
		}
		// PRD 0009 §Functional 2: validate the [runtime] block when
		// present, and require at least one declared export. We accept
		// exports either in [wasm].exports (the v2.0 location) or
		// [runtime].exports (the v2.2 location) — but at least one of
		// the two must be non-empty.
		exports := m.Wasm.Exports
		if m.Runtime != nil && len(m.Runtime.Exports) > 0 {
			exports = m.Runtime.Exports
		}
		if len(exports) == 0 {
			return &errors.Error{
				Component:   Component,
				Problem:     "wasm manifest declares no exports",
				Fix:         "add `[runtime]\\nexports = [\"<fn>\"]` or `[wasm]\\nexports = [\"<fn>\"]`",
				DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
				Recoverable: true,
			}
		}
		for _, e := range exports {
			if IsReservedExport(e) {
				return &errors.Error{
					Component:   Component,
					Problem:     fmt.Sprintf("export %q collides with built-in samuel verb", e),
					Fix:         "rename the wasm export so it does not shadow a samuel command",
					DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
					Recoverable: true,
				}
			}
		}
	}
	if m.Kind == KindOci {
		if m.OCI == nil || strings.TrimSpace(m.OCI.Image) == "" {
			return &errors.Error{
				Component:   Component,
				Problem:     "oci manifest missing [oci] image reference",
				Fix:         "add `[oci] image = \"registry/owner/name:tag\"`",
				DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
				Recoverable: true,
			}
		}
	}

	if m.Samuel.Framework != "" {
		if err := ValidVersionRange(m.Samuel.Framework); err != nil {
			return (&errors.Error{
				Component:   Component,
				Problem:     "invalid samuel.framework version range",
				Fix:         "use cargo-style ranges (^X.Y.Z, ~X.Y.Z, >=X,<Y, *, exact)",
				DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
				Recoverable: true,
			}).Wrap(err)
		}
	}
	if m.Samuel.Protocol != "" {
		if err := ValidVersionRange(m.Samuel.Protocol); err != nil {
			return (&errors.Error{
				Component:   Component,
				Problem:     "invalid samuel.protocol version range",
				DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-MANIFEST-001",
				Recoverable: true,
			}).Wrap(err)
		}
	}
	return nil
}

// ValidName reports whether s matches the plugin-name rule used across
// Samuel: lowercase alphanumerics + dash, 2-64 chars, must start with a
// letter or digit.
func ValidName(s string) bool {
	if len(s) < 2 || len(s) > 64 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' && i != 0 && i != len(s)-1:
		default:
			return false
		}
	}
	return true
}

// reservedExports lists wasm export names that collide with the
// framework's own command verbs. The wasm tier rejects any plugin that
// tries to register one of these.
var reservedExports = map[string]struct{}{
	"init":      {},
	"install":   {},
	"uninstall": {},
	"update":    {},
	"search":    {},
	"info":      {},
	"ls":        {},
	"list":      {},
	"run":       {},
	"doctor":    {},
	"sync":      {},
	"version":   {},
	"new":       {},
}

// IsReservedExport reports whether name shadows a built-in samuel verb.
// Comparison is case-sensitive: wasm exports are lowercase by convention
// and the built-in command names are lowercase verbs.
func IsReservedExport(name string) bool {
	_, ok := reservedExports[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// ValidVersionRange is a thin syntactic check. The full parse lives in
// internal/plugin/semver; this is here to avoid an import cycle between
// the manifest validator and the resolver.
func ValidVersionRange(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("empty range")
	}
	if s == "*" {
		return nil
	}
	switch s[0] {
	case '^', '~', '=', '<', '>':
		return nil
	}
	// allow comma-separated bounded ranges: ">=1.0,<2.0"
	if strings.Contains(s, ",") {
		for _, part := range strings.Split(s, ",") {
			if err := ValidVersionRange(strings.TrimSpace(part)); err != nil {
				return err
			}
		}
		return nil
	}
	// allow plain X.Y.Z (exact)
	if c := s[0]; c >= '0' && c <= '9' {
		return nil
	}
	return fmt.Errorf("unrecognized range syntax: %s", s)
}
