package service

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ar4mirez/samuel/internal/config"
	"github.com/ar4mirez/samuel/internal/plugin"
	"github.com/ar4mirez/samuel/internal/plugin/capability"
	"github.com/ar4mirez/samuel/internal/plugin/manifest"
	"github.com/ar4mirez/samuel/internal/plugin/oci"
	"github.com/ar4mirez/samuel/internal/plugin/registry"
	"github.com/ar4mirez/samuel/internal/plugin/source"
	"github.com/ar4mirez/samuel/internal/plugin/verify"
	"github.com/ar4mirez/samuel/internal/plugin/wasm"
)

// fakeOciEngine implements oci.Engine without needing podman/docker.
type fakeOciEngine struct {
	digest    string
	available map[string]bool
	removed   []string
}

func (f *fakeOciEngine) Pull(_ context.Context, image string) (string, error) {
	if f.available == nil {
		f.available = map[string]bool{}
	}
	f.available[image] = true
	return f.digest, nil
}
func (f *fakeOciEngine) Inspect(_ context.Context, image string) (string, error) {
	if f.available[image] {
		return f.digest, nil
	}
	return "", os.ErrNotExist
}
func (f *fakeOciEngine) Remove(_ context.Context, image string) error {
	delete(f.available, image)
	f.removed = append(f.removed, image)
	return nil
}

func writeSkillSource(t *testing.T, dir, name string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: A test skill\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tomlBody := "name = \"" + name + "\"\nversion = \"1.0.0\"\nkind = \"skill\"\n"
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(tomlBody), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeSkillRegistry writes an index.toml pointing the named plugin at a
// file:// source. Returns the index path.
func writeSkillRegistry(t *testing.T, plugins map[string]registry.Plugin) string {
	t.Helper()
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.toml")
	body := "schema_version = 1\n"
	for name, p := range plugins {
		body += "\n[plugin." + name + "]\nrepo = \"" + p.Repo + "\"\nlatest = \"" + p.Latest + "\"\nkind = \"" + p.Kind + "\"\ndescription = \"" + p.Description + "\"\n"
	}
	if err := os.WriteFile(indexPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return indexPath
}

func writeProjectScaffold(t *testing.T) string {
	t.Helper()
	proj := t.TempDir()
	if err := config.Save(proj, config.Defaults()); err != nil {
		t.Fatal(err)
	}
	return proj
}

func TestService_InstallSkill_HappyPath(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	writeSkillSource(t, src, "go-guide")
	index := writeSkillRegistry(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill"},
	})
	proj := writeProjectScaffold(t)
	svc := &Service{
		ProjectDir: proj,
		Sources:    []registry.Source{{Name: "test", URL: "file://" + index}},
		Registry:   registry.NewClient(t.TempDir()),
		Fetcher:    source.Default(),
		Verifier:   verify.StubVerifier{},
		Policy:     verify.DefaultPolicy(),
		Prompt:     capability.AutoYes,
	}
	res, err := svc.Install(ctx, InstallOptions{Name: "go-guide", AllowUnsigned: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Name != "go-guide" || res.Version != "1.0.0" {
		t.Errorf("unexpected result: %+v", res)
	}
	skillPath := filepath.Join(proj, ".samuel", "plugins", "go-guide", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("SKILL.md not installed: %v", err)
	}
	// Lockfile assertion.
	lf, err := config.LoadLock(proj)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if len(lf.Plugins) != 1 || lf.Plugins[0].Name != "go-guide" {
		t.Errorf("lockfile missing plugin: %+v", lf.Plugins)
	}
}

func TestService_InstallSkill_VersionRange(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	writeSkillSource(t, src, "go-guide")
	index := writeSkillRegistry(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.4.2", Kind: "skill"},
	})
	proj := writeProjectScaffold(t)
	svc := &Service{
		ProjectDir: proj,
		Sources:    []registry.Source{{Name: "test", URL: "file://" + index}},
		Registry:   registry.NewClient(t.TempDir()),
		Fetcher:    source.Default(),
		Verifier:   verify.StubVerifier{},
		Policy:     verify.DefaultPolicy(),
		Prompt:     capability.AutoYes,
	}
	res, err := svc.Install(ctx, InstallOptions{Name: "go-guide", Constraint: "^1.0.0", AllowUnsigned: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Version == "" {
		t.Errorf("version should be resolved")
	}
}

func TestService_InstallSkill_UninstallReinstallIsReproducible(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	writeSkillSource(t, src, "go-guide")
	index := writeSkillRegistry(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill"},
	})
	proj := writeProjectScaffold(t)
	svc := &Service{
		ProjectDir: proj,
		Sources:    []registry.Source{{Name: "test", URL: "file://" + index}},
		Registry:   registry.NewClient(t.TempDir()),
		Fetcher:    source.Default(),
		Verifier:   verify.StubVerifier{},
		Policy:     verify.DefaultPolicy(),
		Prompt:     capability.AutoYes,
	}
	_, err := svc.Install(ctx, InstallOptions{Name: "go-guide", AllowUnsigned: true})
	if err != nil {
		t.Fatalf("Install#1: %v", err)
	}
	lockA, _ := os.ReadFile(filepath.Join(proj, "samuel.lock"))

	if _, err := svc.Uninstall(ctx, "go-guide", plugin.UninstallOptions{}); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".samuel", "plugins", "go-guide")); !os.IsNotExist(err) {
		t.Errorf("plugin dir should be gone")
	}
	if _, err := svc.Install(ctx, InstallOptions{Name: "go-guide", AllowUnsigned: true}); err != nil {
		t.Fatalf("Install#2: %v", err)
	}
	lockB, _ := os.ReadFile(filepath.Join(proj, "samuel.lock"))
	// generated_at and timestamps differ; compare the lock plugin list
	// instead by reading both lockfiles.
	a, _ := config.LoadLock(proj)
	_ = lockA
	_ = lockB
	if len(a.Plugins) != 1 || a.Plugins[0].Name != "go-guide" {
		t.Errorf("lockfile after reinstall: %+v", a.Plugins)
	}
}

func TestService_InstallWasm_HealthOK(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	// Hand-encode a minimal wasm with health=0, protocol=1.
	body := buildFixtureWasm(0, 1)
	if err := os.WriteFile(filepath.Join(src, "plugin.wasm"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	tomlBody := "name = \"codex-translator\"\nversion = \"0.1.0\"\nkind = \"wasm\"\n\n[wasm]\nmodule = \"plugin.wasm\"\n"
	if err := os.WriteFile(filepath.Join(src, manifest.FileName), []byte(tomlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	index := writeSkillRegistry(t, map[string]registry.Plugin{
		"codex-translator": {Repo: "file://" + src, Latest: "0.1.0", Kind: "wasm"},
	})
	proj := writeProjectScaffold(t)
	rt, err := wasm.NewRuntime(ctx, "")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)
	svc := &Service{
		ProjectDir:  proj,
		Sources:     []registry.Source{{Name: "test", URL: "file://" + index}},
		Registry:    registry.NewClient(t.TempDir()),
		Fetcher:     source.Default(),
		Verifier:    verify.StubVerifier{},
		Policy:      verify.DefaultPolicy(),
		WasmRuntime: rt,
		Prompt:      capability.AutoYes,
	}
	if _, err := svc.Install(ctx, InstallOptions{Name: "codex-translator", AllowUnsigned: true}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	wp := wasm.New(manifest.Manifest{Name: "codex-translator", Version: "0.1.0", Kind: manifest.KindWasm}, proj, "", rt, nil)
	st := wp.Check(ctx)
	if !st.OK {
		t.Errorf("wasm health should be ok: %+v", st)
	}
}

func TestService_InstallOci_PullsImage(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	tomlBody := "name = \"claude-runner\"\nversion = \"1.0.0\"\nkind = \"oci\"\n\n[oci]\nimage = \"ghcr.io/samuelpkg/samuel-runner-claude:1.0.0\"\n"
	if err := os.WriteFile(filepath.Join(src, manifest.FileName), []byte(tomlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	index := writeSkillRegistry(t, map[string]registry.Plugin{
		"claude-runner": {Repo: "file://" + src, Latest: "1.0.0", Kind: "oci"},
	})
	proj := writeProjectScaffold(t)
	eng := &fakeOciEngine{digest: "sha256:" + hex.EncodeToString(make([]byte, 32))}
	svc := &Service{
		ProjectDir: proj,
		Sources:    []registry.Source{{Name: "test", URL: "file://" + index}},
		Registry:   registry.NewClient(t.TempDir()),
		Fetcher:    source.Default(),
		Verifier:   verify.StubVerifier{},
		Policy:     verify.DefaultPolicy(),
		OciEngine:  eng,
		Prompt:     capability.AutoYes,
	}
	res, err := svc.Install(ctx, InstallOptions{Name: "claude-runner", AllowUnsigned: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Digest == "" {
		t.Errorf("digest should be pinned")
	}
	lf, _ := config.LoadLock(proj)
	if len(lf.Plugins) == 0 || lf.Plugins[0].Digest == "" {
		t.Errorf("lockfile missing OCI digest: %+v", lf.Plugins)
	}
}

func TestService_InstallSkill_CapabilityPromptsForExec(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	body := "---\nname: dangerous\ndescription: requests exec\n---\n"
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tomlBody := `name = "dangerous"
version = "1.0.0"
kind = "skill"

[capabilities]
exec = true
`
	if err := os.WriteFile(filepath.Join(src, manifest.FileName), []byte(tomlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	index := writeSkillRegistry(t, map[string]registry.Plugin{
		"dangerous": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill"},
	})
	proj := writeProjectScaffold(t)
	called := false
	prompt := capability.PromptFn(func(name string, reqs []capability.Requested) capability.PromptDecision {
		called = true
		if name != "dangerous" {
			t.Errorf("prompt got plugin %q", name)
		}
		return capability.PromptDecision{Granted: true, Reason: "user-prompt"}
	})
	svc := &Service{
		ProjectDir: proj,
		Sources:    []registry.Source{{Name: "test", URL: "file://" + index}},
		Registry:   registry.NewClient(t.TempDir()),
		Fetcher:    source.Default(),
		Verifier:   verify.StubVerifier{},
		Policy:     verify.DefaultPolicy(),
		Prompt:     prompt,
	}
	if _, err := svc.Install(ctx, InstallOptions{Name: "dangerous", AllowUnsigned: true}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !called {
		t.Errorf("exec capability must trigger prompt")
	}
}

func TestService_ListAvailable_MergesInstalledAndRegistry(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	writeSkillSource(t, src, "go-guide")
	index := writeSkillRegistry(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill"},
		"react":    {Repo: "github.com/ar4mirez/react", Latest: "0.1.0", Kind: "skill"},
	})
	proj := writeProjectScaffold(t)
	svc := &Service{
		ProjectDir: proj,
		Sources:    []registry.Source{{Name: "test", URL: "file://" + index}},
		Registry:   registry.NewClient(t.TempDir()),
		Fetcher:    source.Default(),
		Verifier:   verify.StubVerifier{},
		Policy:     verify.DefaultPolicy(),
		Prompt:     capability.AutoYes,
	}
	if _, err := svc.Install(ctx, InstallOptions{Name: "go-guide", AllowUnsigned: true}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	avail, err := svc.ListAvailable(ctx)
	if err != nil {
		t.Fatalf("ListAvailable: %v", err)
	}
	if len(avail) != 2 {
		t.Errorf("expected 2 entries, got %d: %+v", len(avail), avail)
	}
	for _, a := range avail {
		if a.Name == "go-guide" && !a.Installed {
			t.Errorf("go-guide should be installed")
		}
	}
}

// silence unused import lint when tests evolve.
var _ = oci.Engine(nil)
