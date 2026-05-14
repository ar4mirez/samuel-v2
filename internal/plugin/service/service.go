// Package service glues the plugin loader's parts together:
//
//   - registry → resolve <plugin>[@range] → repo + version + manifest
//   - source   → materialize the plugin tree (file:// / git)
//   - verify   → check signature (or honor --allow-unsigned)
//   - capability → derive grants (safe-default | prompt | yes-flag)
//   - tier loader → Install
//   - lockfile → record resolved version + digest + grants
//   - samuel.toml → append/refresh the [[plugins]] entry
//
// The CLI commands stay thin; everything load-bearing lives here so
// tests can drive the full pipeline against an in-memory fake registry
// + file:// sources.
package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/samuelpkg/samuel/internal/config"
	"github.com/samuelpkg/samuel/internal/errors"
	"github.com/samuelpkg/samuel/internal/lock"
	"github.com/samuelpkg/samuel/internal/plugin"
	"github.com/samuelpkg/samuel/internal/plugin/capability"
	"github.com/samuelpkg/samuel/internal/plugin/manifest"
	"github.com/samuelpkg/samuel/internal/plugin/oci"
	"github.com/samuelpkg/samuel/internal/plugin/registry"
	"github.com/samuelpkg/samuel/internal/plugin/skill"
	"github.com/samuelpkg/samuel/internal/plugin/source"
	"github.com/samuelpkg/samuel/internal/plugin/verify"
	"github.com/samuelpkg/samuel/internal/plugin/wasm"
)

// Component is the structured-error namespace.
const Component = "plugin/service"

// Service is the install-side facade.
type Service struct {
	ProjectDir string
	HomeDir    string

	Sources  []registry.Source
	Registry *registry.Client
	Fetcher  source.Fetcher
	Verifier verify.Verifier
	Policy   verify.Policy

	// WasmRuntime is lazily created on first WASM install and reused
	// across the process. Tests inject a runtime with an empty cache dir.
	WasmRuntime *wasm.Runtime

	// OciEngine is the runtime CLI wrapper. nil disables OCI tier (the
	// CLI surfaces an actionable error when needed).
	OciEngine oci.Engine

	// Prompt is the capability-grant prompt. nil means non-interactive.
	Prompt capability.PromptFn
}

// InstallOptions controls one install run.
type InstallOptions struct {
	Name          string
	Constraint    string // version-range, e.g. "^1.0.0"
	AllowPrerelease bool
	AllowUnsigned bool
	Force         bool
	DryRun        bool
	Verbose       bool
	Yes           bool
	NonInteractive bool
	Stdout        any // io.Writer in callers; preserved as any to avoid an import cycle into io
}

// Result is returned after a successful install.
type Result struct {
	Name        string
	Version     string
	Kind        manifest.Kind
	Source      string
	Digest      string
	Grants      []capability.Grant
	Mutations   []plugin.Mutation
	Verified    verify.Result
	Skipped     bool
	AlreadyInstalled bool
}

// Install resolves and installs a single plugin. The caller wraps this
// with cobra UI rendering.
func (s *Service) Install(ctx context.Context, opts InstallOptions) (*Result, error) {
	if s.Verifier == nil {
		s.Verifier = verify.Default()
	}
	if s.Fetcher == nil {
		s.Fetcher = source.Default()
	}

	idx, src, regPlugin, err := s.Registry.FindFirst(ctx, s.Sources, opts.Name)
	if err != nil {
		return nil, err
	}
	_ = idx

	version, err := registry.ResolveVersion(regPlugin, opts.Constraint, opts.AllowPrerelease)
	if err != nil {
		return nil, (&errors.Error{
			Component:   Component,
			Problem:     fmt.Sprintf("cannot resolve version for %s", opts.Name),
			Recoverable: true,
		}).Wrap(err)
	}

	fetched, err := s.Fetcher.Fetch(ctx, source.FetchRequest{
		Repo:    regPlugin.Repo,
		Ref:     version,
		Subpath: regPlugin.Subpath,
		Kind:    regPlugin.Kind,
	})
	if err != nil {
		return nil, err
	}
	defer fetched.Cleanup()

	m, err := manifest.LoadFromDir(fetched.Root)
	if err != nil {
		return nil, err
	}
	// Allow the registry's `kind` hint to seed the manifest when the
	// source omitted it.
	if m.Kind == "" && regPlugin.Kind != "" {
		m.Kind = manifest.Kind(regPlugin.Kind)
	}
	if m.Version == "" {
		m.Version = version
	}

	verReq := verify.Request{
		Policy:        effectivePolicy(s.Policy, regPlugin.Upstream),
		Plugin:        opts.Name,
		Source:        regPlugin.Repo,
		Registry:      src.Name,
		AllowUnsigned: opts.AllowUnsigned,
	}
	verRes, err := s.verifyArtifact(ctx, fetched.Root, m, verReq)
	if err != nil {
		return nil, err
	}

	caps := capability.FromManifest(m)
	grants, ok, err := capability.Decide(opts.Name, caps, s.Prompt, capability.DecideOptions{
		YesAll:         opts.Yes,
		NonInteractive: opts.NonInteractive,
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &errors.Error{
			Component:   Component,
			Problem:     "user declined required capabilities",
			Fix:         "re-run with --yes or fewer required capabilities",
			Recoverable: true,
		}
	}

	plg, err := s.buildPlugin(ctx, *m, fetched.Root, grants)
	if err != nil {
		return nil, err
	}

	pluginInstallOpts := plugin.InstallOptions{
		DryRun:  opts.DryRun,
		Force:   opts.Force,
		Verbose: opts.Verbose,
	}
	instRes, err := plg.Install(ctx, pluginInstallOpts)
	if err != nil {
		return nil, err
	}

	if !opts.DryRun {
		if err := s.recordLockfile(*m, regPlugin, src, grants, verRes, instRes, plg); err != nil {
			return nil, err
		}
		if err := s.appendConfig(*m); err != nil {
			return nil, err
		}
	}

	digest := ""
	if op, ok := plg.(*oci.Plugin); ok {
		digest = op.Digest
	}

	return &Result{
		Name:             m.Name,
		Version:          m.Version,
		Kind:             m.Kind,
		Source:           regPlugin.Repo,
		Digest:           digest,
		Grants:           grants,
		Mutations:        instRes.Mutations,
		Verified:         verRes,
		Skipped:          instRes.Skipped,
		AlreadyInstalled: instRes.AlreadyInstalled,
	}, nil
}

// effectivePolicy mixes the global policy with the per-registry-entry
// upstream flag — upstream plugins (mcp-builder et al.) bypass the
// signed-default requirement.
func effectivePolicy(base verify.Policy, upstream bool) verify.Policy {
	if upstream {
		base.SignedDefault = false
	}
	return base
}

// verifyArtifact dispatches to the right Verifier method per tier.
func (s *Service) verifyArtifact(ctx context.Context, root string, m *manifest.Manifest, req verify.Request) (verify.Result, error) {
	switch m.Kind {
	case manifest.KindSkill:
		// Verify the SKILL.md as the artifact (skill payload is the file).
		return s.Verifier.VerifyBlob(ctx, filepath.Join(root, skill.SkillFile), req)
	case manifest.KindWasm:
		modPath := filepath.Join(root, wasm.ModuleFileName)
		if m.Wasm != nil && m.Wasm.Module != "" {
			modPath = filepath.Join(root, m.Wasm.Module)
		}
		return s.Verifier.VerifyBlob(ctx, modPath, req)
	case manifest.KindOci:
		// Digest is pinned post-pull; verify after install, but we
		// honor the policy here against the manifest image ref.
		digest := ""
		if m.OCI != nil {
			digest = m.OCI.Digest
		}
		return s.Verifier.VerifyImage(ctx, digest, req)
	}
	return verify.Result{}, &errors.Error{
		Component:   Component,
		Problem:     "unsupported plugin kind",
		Cause:       string(m.Kind),
		Recoverable: true,
	}
}

// buildPlugin constructs the right tier-specific plugin.Plugin.
func (s *Service) buildPlugin(ctx context.Context, m manifest.Manifest, sourceDir string, grants []capability.Grant) (plugin.Plugin, error) {
	switch m.Kind {
	case manifest.KindSkill:
		return skill.New(m, s.ProjectDir, sourceDir), nil
	case manifest.KindWasm:
		if s.WasmRuntime == nil {
			rt, err := wasm.NewRuntime(ctx, "")
			if err != nil {
				return nil, err
			}
			s.WasmRuntime = rt
		}
		return wasm.New(m, s.ProjectDir, sourceDir, s.WasmRuntime, grants), nil
	case manifest.KindOci:
		if s.OciEngine == nil {
			return nil, &errors.Error{
				Component:   Component,
				Problem:     "OCI plugin install requires a container runtime",
				Fix:         "install podman or docker, or set SAMUEL_RUNTIME",
				Recoverable: true,
			}
		}
		return oci.New(m, s.ProjectDir, s.OciEngine, grants), nil
	}
	return nil, &errors.Error{
		Component:   Component,
		Problem:     "unknown plugin kind",
		Cause:       string(m.Kind),
		Recoverable: true,
	}
}

func (s *Service) recordLockfile(m manifest.Manifest, p registry.Plugin, src registry.Source, grants []capability.Grant, vres verify.Result, instRes plugin.InstallResult, plg plugin.Plugin) error {
	lf, err := lock.ReadLockfile(s.ProjectDir)
	if err != nil {
		return err
	}
	// Remove any prior entry for this plugin so re-install overwrites.
	filtered := lf.Plugins[:0]
	for _, lp := range lf.Plugins {
		if lp.Name != m.Name {
			filtered = append(filtered, lp)
		}
	}
	digest := ""
	if op, ok := plg.(*oci.Plugin); ok && op.Digest != "" {
		digest = op.Digest
	}
	caps := make([]string, 0, len(grants))
	for _, g := range grants {
		caps = append(caps, string(g.Kind))
	}
	filtered = append(filtered, config.LockedPlugin{
		Name:         m.Name,
		Version:      m.Version,
		Kind:         string(m.Kind),
		Digest:       digest,
		Source:       p.Repo,
		Capabilities: caps,
		Signed:       vres.Verified,
	})
	lf.Plugins = filtered
	now := time.Now().UTC().Format(time.RFC3339)
	for _, mu := range instRes.Mutations {
		lf.Mutations = append(lf.Mutations, lock.ToRecord(m.Name, mu, now))
	}
	_ = src
	return lock.WriteLockfile(s.ProjectDir, lf)
}

// appendConfig refreshes the [[plugins]] entry for m in samuel.toml.
func (s *Service) appendConfig(m manifest.Manifest) error {
	cfg, err := config.Load(s.ProjectDir)
	if err != nil {
		if !stderrors.Is(err, config.ErrNotFound) {
			return err
		}
		cfg = config.Defaults()
	}
	updated := cfg.Plugins[:0]
	for _, pe := range cfg.Plugins {
		if pe.Name != m.Name {
			updated = append(updated, pe)
		}
	}
	updated = append(updated, config.PluginEntry{
		Name:    m.Name,
		Version: m.Version,
		Kind:    string(m.Kind),
	})
	cfg.Plugins = updated
	return config.Save(s.ProjectDir, cfg)
}

// Uninstall reverses the plugin's lockfile mutations, removes the
// [[plugins]] entry, and runs the tier's Uninstall.
func (s *Service) Uninstall(ctx context.Context, name string, opts plugin.UninstallOptions) (*Result, error) {
	lf, err := lock.ReadLockfile(s.ProjectDir)
	if err != nil {
		return nil, err
	}
	var locked *config.LockedPlugin
	for i := range lf.Plugins {
		if lf.Plugins[i].Name == name {
			locked = &lf.Plugins[i]
			break
		}
	}
	if locked == nil {
		// Best-effort: remove from samuel.toml even when the lockfile
		// has nothing recorded.
		_ = s.removeFromConfig(name)
		return &Result{Name: name, Skipped: true}, nil
	}
	m := manifest.Manifest{
		Name: locked.Name, Version: locked.Version, Kind: manifest.Kind(locked.Kind),
	}
	if m.Kind == manifest.KindOci {
		// Reconstruct the OCI block from the lockfile source.
		m.OCI = &manifest.OCIBlock{Image: locked.Source, Digest: locked.Digest}
	}
	plg, err := s.buildPlugin(ctx, m, "", nil)
	if err != nil {
		return nil, err
	}
	uninst, err := plg.Uninstall(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Replay mutations in reverse — best-effort, errors are recorded.
	if err := s.reverseMutations(name, lf); err != nil {
		return nil, err
	}
	// Drop the plugin from lockfile + config.
	keep := lf.Plugins[:0]
	for _, lp := range lf.Plugins {
		if lp.Name != name {
			keep = append(keep, lp)
		}
	}
	lf.Plugins = keep
	keepM := lf.Mutations[:0]
	for _, mu := range lf.Mutations {
		if mu.Plugin != name {
			keepM = append(keepM, mu)
		}
	}
	lf.Mutations = keepM
	if err := lock.WriteLockfile(s.ProjectDir, lf); err != nil {
		return nil, err
	}
	if err := s.removeFromConfig(name); err != nil {
		return nil, err
	}
	return &Result{
		Name:      name,
		Version:   locked.Version,
		Kind:      manifest.Kind(locked.Kind),
		Source:    locked.Source,
		Mutations: uninst.Mutations,
		Skipped:   uninst.Skipped,
	}, nil
}

func (s *Service) reverseMutations(name string, lf *config.Lockfile) error {
	// For v2.0 we trust the tier's Uninstall to clean up; per-mutation
	// reversal hooks land in v2.1 (Mutation.Reverse closures don't
	// survive across processes). The plugin install dir is removed by
	// the tier's Uninstall, so dropping the audit-log entries here is
	// the only remaining step.
	_ = name
	_ = lf
	return nil
}

func (s *Service) removeFromConfig(name string) error {
	cfg, err := config.Load(s.ProjectDir)
	if err != nil {
		if stderrors.Is(err, config.ErrNotFound) {
			return nil
		}
		return err
	}
	keep := cfg.Plugins[:0]
	for _, pe := range cfg.Plugins {
		if pe.Name != name {
			keep = append(keep, pe)
		}
	}
	cfg.Plugins = keep
	return config.Save(s.ProjectDir, cfg)
}

// ListInstalled reads samuel.toml [[plugins]] and returns the entries.
func (s *Service) ListInstalled() ([]config.PluginEntry, error) {
	cfg, err := config.Load(s.ProjectDir)
	if err != nil {
		if stderrors.Is(err, config.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return cfg.Plugins, nil
}

// ListAvailable merges installed + registry entries. Used by `samuel ls --all`.
func (s *Service) ListAvailable(ctx context.Context) ([]AvailableEntry, error) {
	installed, err := s.ListInstalled()
	if err != nil {
		return nil, err
	}
	installedByName := map[string]config.PluginEntry{}
	for _, e := range installed {
		installedByName[e.Name] = e
	}
	out := []AvailableEntry{}
	seen := map[string]bool{}
	for _, src := range s.Sources {
		idx, err := s.Registry.LoadIndex(ctx, src, false)
		if err != nil {
			continue
		}
		for name, p := range idx.Plugins {
			if seen[name] {
				continue
			}
			seen[name] = true
			entry := AvailableEntry{Name: name, Source: src.Name, Plugin: p}
			if inst, ok := installedByName[name]; ok {
				entry.Installed = true
				entry.InstalledVersion = inst.Version
			}
			out = append(out, entry)
		}
	}
	// Append installed-only entries (not in any registry).
	for _, e := range installed {
		if !seen[e.Name] {
			out = append(out, AvailableEntry{
				Name:             e.Name,
				Installed:        true,
				InstalledVersion: e.Version,
			})
		}
	}
	return out, nil
}

// AvailableEntry mixes registry + installed state for `samuel ls --all`
// and `samuel update`.
type AvailableEntry struct {
	Name             string
	Source           string
	Plugin           registry.Plugin
	Installed        bool
	InstalledVersion string
}

// HasUpdate reports whether the installed version is older than the
// registry's latest. Empty values are treated as "no update".
func (a AvailableEntry) HasUpdate() bool {
	if !a.Installed || a.InstalledVersion == "" || a.Plugin.Latest == "" {
		return false
	}
	return a.InstalledVersion != a.Plugin.Latest
}

// EnsureProjectInitialized returns an actionable error when there is no
// samuel.toml in ProjectDir — most commands surface this before doing
// any registry work.
func (s *Service) EnsureProjectInitialized() error {
	if _, err := os.Stat(filepath.Join(s.ProjectDir, config.ProjectFile)); err != nil {
		return &errors.Error{
			Component:   Component,
			Problem:     "no samuel.toml in current directory",
			Fix:         "run `samuel init` first",
			Recoverable: true,
		}
	}
	return nil
}
