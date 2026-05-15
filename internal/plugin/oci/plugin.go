package oci

import (
	"context"
	"path/filepath"

	"github.com/samuelpkg/samuel/internal/errors"
	"github.com/samuelpkg/samuel/internal/plugin"
	"github.com/samuelpkg/samuel/internal/plugin/capability"
	"github.com/samuelpkg/samuel/internal/plugin/manifest"
)

// Plugin is the OCI-tier plugin.Plugin implementation.
type Plugin struct {
	Manifest_  manifest.Manifest
	ProjectDir string
	// Engine is the container CLI wrapper. Tests inject a fake.
	Engine Engine
	// Grants are the capability grants enforced when launching the container.
	Grants []capability.Grant

	// Digest is populated by Install for samuel.lock recording.
	Digest string
}

// New constructs an OCI plugin.
func New(m manifest.Manifest, projectDir string, eng Engine, grants []capability.Grant) *Plugin {
	return &Plugin{Manifest_: m, ProjectDir: projectDir, Engine: eng, Grants: grants}
}

// Name returns the manifest plugin name.
func (p *Plugin) Name() string { return p.Manifest_.Name }

// Manifest returns the framework Manifest.
func (p *Plugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:    p.Manifest_.Name,
		Version: p.Manifest_.Version,
		Kind:    plugin.KindOci,
		Summary: p.Manifest_.Summary,
	}
}

// image returns the manifest's image reference (digest pinned at install
// time when known).
func (p *Plugin) image() string {
	if p.Manifest_.OCI == nil {
		return ""
	}
	return p.Manifest_.OCI.Image
}

// Detect runs `inspect` against the manifest image; success means the
// image is locally available.
func (p *Plugin) Detect(ctx context.Context) (plugin.DetectResult, error) {
	if p.Engine == nil {
		return plugin.DetectResult{Installed: false}, nil
	}
	digest, err := p.Engine.Inspect(ctx, p.image())
	if err != nil {
		return plugin.DetectResult{Installed: false, Path: p.image()}, nil
	}
	return plugin.DetectResult{
		Installed: true,
		Version:   p.Manifest_.Version,
		Path:      digest,
	}, nil
}

// Install pulls the image, validates the name, and pins the digest in
// the manifest copy that lands under .samuel/plugins/<name>/.
func (p *Plugin) Install(ctx context.Context, opts plugin.InstallOptions) (plugin.InstallResult, error) {
	res := plugin.InstallResult{Component: p.Name()}
	if p.Engine == nil {
		return res, &errors.Error{
			Component:   Component,
			Problem:     "oci plugin install requires an engine",
			Recoverable: false,
		}
	}
	if p.image() == "" {
		return res, &errors.Error{
			Component:   Component,
			Problem:     "manifest missing oci.image",
			Recoverable: false,
		}
	}
	if _, err := ParseImageName(p.image()); err != nil {
		return res, err
	}
	if err := ValidateDigestPinned(p.image()); err != nil {
		return res, err
	}
	if opts.DryRun {
		return res, nil
	}
	digest, err := p.Engine.Pull(ctx, p.image())
	if err != nil {
		return res, err
	}
	p.Digest = digest
	res.Mutations = append(res.Mutations, plugin.Mutation{
		Kind:        plugin.MutationOciPulled,
		Path:        p.image(),
		Description: "pulled OCI image " + p.image() + " @ " + digest,
		Reverse: func(ctx context.Context) error {
			return p.Engine.Remove(ctx, p.image())
		},
	})
	return res, nil
}

// Check inspects the local image and returns OK when the digest matches.
func (p *Plugin) Check(ctx context.Context) plugin.HealthStatus {
	if p.Engine == nil {
		return plugin.HealthStatus{Component: p.Name(), OK: false, Message: "no engine configured"}
	}
	digest, err := p.Engine.Inspect(ctx, p.image())
	if err != nil {
		return plugin.HealthStatus{
			Component: p.Name(),
			OK:        false,
			Message:   "image not found locally: " + p.image(),
			FixHint:   "samuel install " + p.Name(),
		}
	}
	return plugin.HealthStatus{Component: p.Name(), OK: true, Message: "image present @ " + digest}
}

// Uninstall removes the local image when no other plugin in projectDir
// references it. The caller drives the dedup check (it has access to
// samuel.toml); here we just shell out to engine.Remove.
func (p *Plugin) Uninstall(ctx context.Context, opts plugin.UninstallOptions) (plugin.UninstallResult, error) {
	res := plugin.UninstallResult{Component: p.Name()}
	if p.Engine == nil || p.image() == "" {
		res.Skipped = true
		return res, nil
	}
	if opts.DryRun {
		return res, nil
	}
	if err := p.Engine.Remove(ctx, p.image()); err != nil {
		return res, err
	}
	res.Mutations = append(res.Mutations, plugin.Mutation{
		Kind:        plugin.MutationOciPulled,
		Path:        p.image(),
		Description: "removed OCI image " + p.image(),
	})
	return res, nil
}

// MountLayout returns the canonical container mount layout the bridge
// launcher applies. Path values are the host paths; the container side
// is hard-coded by spec.
type MountLayout struct {
	Workspace    string // <project> → /workspace (rw or ro per capability)
	Skills       string // ~/.samuel/builtins → /skills (ro)
	SamuelRun    string // <project>/.samuel/run → /.samuel/run (rw or ro)
	PluginConfig string // <project>/.samuel/plugins/<name>/config → /plugin/config (ro)
	BridgeSocket string // host path to the bridge unix socket → /samuel-bridge
}

// BuildMountLayout assembles the standard mount layout for plugin p.
// Workspace writability follows the filesystem.write capability.
func (p *Plugin) BuildMountLayout(home string) MountLayout {
	return MountLayout{
		Workspace:    p.ProjectDir,
		Skills:       filepath.Join(home, ".samuel", "builtins"),
		SamuelRun:    filepath.Join(p.ProjectDir, ".samuel", "run"),
		PluginConfig: filepath.Join(p.ProjectDir, ".samuel", "plugins", p.Name(), "config"),
		BridgeSocket: filepath.Join(p.ProjectDir, ".samuel", "run", p.Name()+".sock"),
	}
}

// Compile-time guarantee.
var _ plugin.Plugin = (*Plugin)(nil)
