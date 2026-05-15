// Package config models samuel.toml (the user-edited project config)
// and samuel.lock (the machine-managed resolved-plugin lockfile).
//
// Schemas match the design in .wiki/entities/config-format.md. TOML is
// the source of truth; YAML is intentionally NOT supported in v2 — the
// "TOML primary, YAML secondary" wording in v1 docs was relaxed for
// the v2 launch (one format, one parser, no ambiguity).
//
// Both files use pelletier/go-toml v2 for marshal/unmarshal. Save uses
// the atomic write-tmp-then-rename pattern v1 hardened in its
// orchestrator port — half-written config files are the canonical way
// to lose a user's setup.
package config

// FileName values are the on-disk names callers pass to Load/Save.
const (
	ProjectFile = "samuel.toml"
	LockFile    = "samuel.lock"
)

// SchemaVersion is the version stamped into newly-written samuel.toml
// files. Bumped when the schema gains breaking changes.
const SchemaVersion = "1"

// Config is the parsed samuel.toml.
//
// The structure follows the wiki entity:
//
//	version = "1"
//	default_methodology = "ralph"
//	[[plugins]] name="…" version="…" kind="skill|wasm|oci"
//	[methodology.<name>] enabled=… agent=… max_iterations=… quality_checks=[…]
//	[guardrails] max_function_lines=… max_file_lines=… require_tests=…
//	[[registries]] name="…" url="…" default=true
//	[oci] cache_budget = "10g"
type Config struct {
	Version            string                 `toml:"version"`
	DefaultMethodology string                 `toml:"default_methodology,omitempty"`
	Plugins            []PluginEntry          `toml:"plugins,omitempty"`
	Methodology        map[string]Methodology `toml:"methodology,omitempty"`
	Guardrails         *Guardrails            `toml:"guardrails,omitempty"`
	Registries         []Registry             `toml:"registries,omitempty"`
	Translators        *Translators           `toml:"translators,omitempty"`
	OCI                *OCIConfig             `toml:"oci,omitempty"`
}

// OCIConfig governs the user-scoped OCI tier settings (PRD 0010
// §Functional 12.4). cache_budget is a size string ("10g", "500m")
// the image-cache LRU consults before evicting old entries; the empty
// string falls back to the 10 GB default.
type OCIConfig struct {
	CacheBudget string `toml:"cache_budget,omitempty"`
}

// PluginEntry is one [[plugins]] block in samuel.toml.
type PluginEntry struct {
	Name    string `toml:"name"`
	Version string `toml:"version,omitempty"`
	Kind    string `toml:"kind"`
	Source  string `toml:"source,omitempty"`
}

// Methodology is one [methodology.<name>] block. The map key on the
// parent Config carries the methodology name (e.g. "ralph"), so Name
// is not stored here.
type Methodology struct {
	Enabled       bool     `toml:"enabled"`
	Agent         string   `toml:"agent,omitempty"`
	MaxIterations int      `toml:"max_iterations,omitempty"`
	QualityChecks []string `toml:"quality_checks,omitempty"`
	Encoding      Encoding `toml:"encoding,omitempty"`
}

// Encoding pins per-file encodings for a methodology's runtime files.
// Defaults: structured=toon, progress=md (see toon-evaluation memo).
type Encoding struct {
	Structured string `toml:"structured,omitempty"`
	Progress   string `toml:"progress,omitempty"`
}

// Guardrails is the [guardrails] block — code-quality limits used by
// the methodology's quality checks and the AGENTS.md template.
type Guardrails struct {
	MaxFunctionLines int  `toml:"max_function_lines,omitempty"`
	MaxFileLines     int  `toml:"max_file_lines,omitempty"`
	RequireTests     bool `toml:"require_tests,omitempty"`
}

// Registry is one [[registries]] block — a remote plugin index.
type Registry struct {
	Name    string `toml:"name,omitempty"`
	URL     string `toml:"url"`
	Default bool   `toml:"default,omitempty"`
}

// Translators groups built-in translator settings. Today the only
// built-in translator is the Claude AGENTS.md → CLAUDE.md mirror; every
// other tool ships as a registry plugin. The carve-out exists because
// Claude Code is the lone outlier that does not read AGENTS.md
// natively, and shipping the trivial mirror as a plugin was friction
// without payoff.
type Translators struct {
	Claude *ClaudeTranslator `toml:"claude,omitempty"`
}

// ClaudeTranslator configures the built-in AGENTS.md → CLAUDE.md
// mirror. Enabled defaults to true.
type ClaudeTranslator struct {
	Enabled bool `toml:"enabled"`
}

// ClaudeTranslatorEnabled reports whether the built-in AGENTS.md →
// CLAUDE.md mirror should run for this project.
//
// The translator is a built-in default-on feature, so absent
// configuration MUST be treated as enabled. The [translators.claude]
// section exists for explicit opt-out (enabled = false), not opt-in.
// Without this rule, every project initialized before the translator
// landed would silently stop receiving mirror updates after an upgrade.
func (c *Config) ClaudeTranslatorEnabled() bool {
	if c == nil {
		return false
	}
	if c.Translators == nil || c.Translators.Claude == nil {
		return true
	}
	return c.Translators.Claude.Enabled
}

// Defaults returns the zero-value-with-sensible-defaults Config used
// when a project has no samuel.toml yet (e.g. before `samuel init`).
func Defaults() *Config {
	return &Config{
		Version:            SchemaVersion,
		DefaultMethodology: "ralph",
		Methodology: map[string]Methodology{
			"ralph": {
				Enabled:       true,
				Agent:         "claude",
				MaxIterations: 25,
				Encoding:      Encoding{Structured: "toon", Progress: "md"},
			},
		},
		Guardrails: &Guardrails{
			MaxFunctionLines: 50,
			MaxFileLines:     300,
			RequireTests:     true,
		},
		Registries: []Registry{
			{Name: "official", URL: "github.com/samuelpkg/samuel-registry", Default: true},
		},
		Translators: &Translators{
			Claude: &ClaudeTranslator{Enabled: true},
		},
	}
}

// Lockfile models samuel.lock.
//
// Two record sets coexist in the lockfile:
//   - Plugins: resolved-version + digest entries (PRD 0001 schema, used
//     by the registry / version resolver).
//   - Mutations: an append-only audit log written by the orchestrator
//     during Install / Uninstall (PRD 0002). Each entry mirrors a
//     plugin.Mutation so `samuel uninstall` can reverse what was applied
//     without rerunning Detect on every plugin.
type Lockfile struct {
	Version      string           `toml:"version"`
	GeneratedAt  string           `toml:"generated_at,omitempty"`
	TOONSpec     string           `toml:"toon_spec,omitempty"`
	Plugins      []LockedPlugin   `toml:"plugins,omitempty"`
	Mutations    []MutationRecord `toml:"mutations,omitempty"`
	Capabilities []string         `toml:"capabilities,omitempty"`
	// Agents pins the per-adapter container image digest (PRD 0010
	// §Functional 7.5–7.7). `samuel update --agents` re-resolves the
	// digests from each manifest's published tag.
	Agents []LockedAgent `toml:"agents,omitempty"`
}

// LockedAgent records an adapter's pinned container image. Adapter is
// the adapter name (e.g. "claude"); Image is the digest-pinned OCI
// reference.
type LockedAgent struct {
	Adapter string `toml:"adapter"`
	Image   string `toml:"image"`
	Digest  string `toml:"digest,omitempty"`
}

// LockedPlugin is the resolved-version + signature record for one
// installed plugin.
type LockedPlugin struct {
	Name         string   `toml:"name"`
	Version      string   `toml:"version"`
	Kind         string   `toml:"kind"`
	Digest       string   `toml:"digest,omitempty"`
	Source       string   `toml:"source,omitempty"`
	Capabilities []string `toml:"capabilities,omitempty"`
	Signed       bool     `toml:"signed,omitempty"`
}

// MutationRecord is one entry in the lockfile's mutation audit log.
// Each Install on a plugin appends one record per state change so the
// orchestrator can reverse them on uninstall without rerunning Detect.
//
// Reverse functions are NOT serialized — the kind + path are enough for
// `samuel uninstall` to dispatch the correct undo (delete file, remove
// symlink, etc.). The schema is forward-compatible: unknown kinds in
// older binaries are reported as skipped rather than fatal.
type MutationRecord struct {
	// Plugin is the component that produced this mutation
	// (plugin.Plugin.Name()).
	Plugin string `toml:"plugin"`
	// Kind is the plugin.MutationKind string value.
	Kind string `toml:"kind"`
	// Path is the resource affected (file, symlink, dir, command name).
	Path string `toml:"path"`
	// Description is the human-readable note from plugin.Mutation.
	Description string `toml:"description,omitempty"`
	// AppliedAt is RFC3339 — when the orchestrator applied this entry.
	AppliedAt string `toml:"applied_at,omitempty"`
}
