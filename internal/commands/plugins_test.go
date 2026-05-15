package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelpkg/samuel/internal/config"
	"github.com/samuelpkg/samuel/internal/plugin/capability"
	"github.com/samuelpkg/samuel/internal/plugin/manifest"
	"github.com/samuelpkg/samuel/internal/plugin/oci"
	"github.com/samuelpkg/samuel/internal/plugin/registry"
	"github.com/samuelpkg/samuel/internal/ui"
)

// fakeOciEngine without container runtime.
type fakeOciEngine struct {
	digest    string
	available map[string]bool
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
	return nil
}

// pinTestEnv returns a chdir-cleanup that scopes test output capture.
func pinTestEnv(t *testing.T) (cleanup func()) {
	t.Helper()
	prev, _ := os.Getwd()
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	ui.SetWriters(stdoutBuf, stderrBuf)
	return func() {
		_ = os.Chdir(prev)
		ui.SetWriters(os.Stdout, os.Stderr)
		ResetFlagsForTest()
	}
}

func writeSkillFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := "---\nname: go-guide\ndescription: Go style guardrails\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tomlBody := "name = \"go-guide\"\nversion = \"1.0.0\"\nkind = \"skill\"\n"
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(tomlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeRegistryFixture(t *testing.T, plugins map[string]registry.Plugin) string {
	t.Helper()
	dir := t.TempDir()
	body := "schema_version = 1\n"
	for name, p := range plugins {
		body += "\n[plugin." + name + "]\nrepo = \"" + p.Repo + "\"\nlatest = \"" + p.Latest + "\"\nkind = \"" + p.Kind + "\"\ndescription = \"" + p.Description + "\"\n"
	}
	indexPath := filepath.Join(dir, "index.toml")
	if err := os.WriteFile(indexPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return indexPath
}

func setupProject(t *testing.T) string {
	t.Helper()
	proj := t.TempDir()
	if err := config.Save(proj, config.Defaults()); err != nil {
		t.Fatal(err)
	}
	return proj
}

func TestCLI_InstallSkill_HappyPath(t *testing.T) {
	defer pinTestEnv(t)()
	src := writeSkillFixture(t)
	idx := writeRegistryFixture(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill"},
	})
	proj := setupProject(t)
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	testRegistrySources = []registry.Source{{Name: "test", URL: "file://" + idx}}
	testPrompt = capability.AutoYes
	defer func() { testRegistrySources = nil; testPrompt = nil }()

	rootCmd.SetArgs([]string{"install", "go-guide", "--allow-unsigned"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	skillPath := filepath.Join(proj, ".samuel", "plugins", "go-guide", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("expected SKILL.md installed: %v", err)
	}
	cfg, err := config.Load(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Name != "go-guide" {
		t.Errorf("samuel.toml plugin entry missing: %+v", cfg.Plugins)
	}
}

func TestCLI_InstallDryRun_DoesNotClaimInstalled(t *testing.T) {
	// Regression for issue #9: rc.13 install --dry-run printed
	// `✓ Installed go-guide@1.0.0` even though nothing was written.
	// A user reading the output would conclude the install happened.
	// The dry-run path now uses `(dry-run) Would install …`.
	defer pinTestEnv(t)()
	src := writeSkillFixture(t)
	idx := writeRegistryFixture(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill"},
	})
	proj := setupProject(t)
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	testRegistrySources = []registry.Source{{Name: "test", URL: "file://" + idx}}
	testPrompt = capability.AutoYes
	defer func() { testRegistrySources = nil; testPrompt = nil }()

	out := &bytes.Buffer{}
	ui.SetWriters(out, out)
	defer ui.SetWriters(os.Stdout, os.Stderr)

	rootCmd.SetArgs([]string{"install", "go-guide", "--allow-unsigned", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	text := out.String()
	if strings.Contains(text, "✓ Installed go-guide") {
		t.Errorf("dry-run output wrongly claims `Installed`; got %q", text)
	}
	if !strings.Contains(text, "(dry-run)") {
		t.Errorf("dry-run output should mark itself; got %q", text)
	}
	if !strings.Contains(text, "Would install go-guide") {
		t.Errorf("dry-run output should preview the install; got %q", text)
	}

	// Side-effect assertion: nothing was actually installed.
	if _, err := os.Stat(filepath.Join(proj, ".samuel", "plugins", "go-guide")); !os.IsNotExist(err) {
		t.Errorf("dry-run should not write plugin dir; got err=%v", err)
	}
}

func TestCLI_InstallSkill_VersionRange(t *testing.T) {
	defer pinTestEnv(t)()
	src := writeSkillFixture(t)
	idx := writeRegistryFixture(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill"},
	})
	proj := setupProject(t)
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	testRegistrySources = []registry.Source{{Name: "test", URL: "file://" + idx}}
	testPrompt = capability.AutoYes
	defer func() { testRegistrySources = nil; testPrompt = nil }()

	rootCmd.SetArgs([]string{"install", "go-guide@^1.0.0", "--allow-unsigned"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestCLI_InstallOci_PinsDigest(t *testing.T) {
	defer pinTestEnv(t)()
	src := t.TempDir()
	digestHex := strings.Repeat("a", 64)
	tomlBody := "name = \"claude-runner\"\nversion = \"1.0.0\"\nkind = \"oci\"\n\n[oci]\nimage = \"ghcr.io/samuelpkg/samuel-runner-claude@sha256:" + digestHex + "\"\n"
	if err := os.WriteFile(filepath.Join(src, manifest.FileName), []byte(tomlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := writeRegistryFixture(t, map[string]registry.Plugin{
		"claude-runner": {Repo: "file://" + src, Latest: "1.0.0", Kind: "oci"},
	})
	proj := setupProject(t)
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	testRegistrySources = []registry.Source{{Name: "test", URL: "file://" + idx}}
	testPrompt = capability.AutoYes
	testOciEngine = &fakeOciEngine{digest: "sha256:" + strings.Repeat("a", 64)}
	defer func() {
		testRegistrySources = nil
		testPrompt = nil
		testOciEngine = nil
	}()
	rootCmd.SetArgs([]string{"install", "claude-runner", "--allow-unsigned"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lf, err := config.LoadLock(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(lf.Plugins) == 0 || lf.Plugins[0].Digest == "" {
		t.Errorf("digest not pinned in lockfile: %+v", lf.Plugins)
	}
}

func TestCLI_UninstallReversesInstall(t *testing.T) {
	defer pinTestEnv(t)()
	src := writeSkillFixture(t)
	idx := writeRegistryFixture(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill"},
	})
	proj := setupProject(t)
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	testRegistrySources = []registry.Source{{Name: "test", URL: "file://" + idx}}
	testPrompt = capability.AutoYes
	defer func() { testRegistrySources = nil; testPrompt = nil }()

	rootCmd.SetArgs([]string{"install", "go-guide", "--allow-unsigned"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}
	ResetFlagsForTest()
	rootCmd.SetArgs([]string{"uninstall", "go-guide"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".samuel", "plugins", "go-guide")); !os.IsNotExist(err) {
		t.Errorf("plugin dir should be removed")
	}
	cfg, _ := config.Load(proj)
	if len(cfg.Plugins) != 0 {
		t.Errorf("samuel.toml should not list plugin: %+v", cfg.Plugins)
	}
}

func TestCLI_LsAndSearchAndInfo(t *testing.T) {
	defer pinTestEnv(t)()
	src := writeSkillFixture(t)
	idx := writeRegistryFixture(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill", Description: "Go style guardrails"},
		"react":    {Repo: "github.com/ar4mirez/react", Latest: "0.1.0", Kind: "skill", Description: "React helper"},
	})
	proj := setupProject(t)
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	testRegistrySources = []registry.Source{{Name: "test", URL: "file://" + idx}}
	testPrompt = capability.AutoYes
	defer func() { testRegistrySources = nil; testPrompt = nil }()

	rootCmd.SetArgs([]string{"install", "go-guide", "--allow-unsigned"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}
	ResetFlagsForTest()

	// ls — default view
	rootCmd.SetArgs([]string{"ls"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	ResetFlagsForTest()

	// ls --all
	rootCmd.SetArgs([]string{"ls", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("ls --all: %v", err)
	}
	ResetFlagsForTest()

	// search react
	rootCmd.SetArgs([]string{"search", "react"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("search: %v", err)
	}
	ResetFlagsForTest()

	// info react (registry-only)
	rootCmd.SetArgs([]string{"info", "react"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("info: %v", err)
	}
	ResetFlagsForTest()

	// info go-guide (installed)
	rootCmd.SetArgs([]string{"info", "go-guide"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("info installed: %v", err)
	}
	ResetFlagsForTest()

	// update no-args
	rootCmd.SetArgs([]string{"update"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update: %v", err)
	}
}

func TestCLI_InstallBareInvocationListsInstalled(t *testing.T) {
	defer pinTestEnv(t)()
	proj := setupProject(t)
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"install"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestCLI_NoClaudeOrAgentArtifactsWritten(t *testing.T) {
	defer pinTestEnv(t)()
	src := writeSkillFixture(t)
	idx := writeRegistryFixture(t, map[string]registry.Plugin{
		"go-guide": {Repo: "file://" + src, Latest: "1.0.0", Kind: "skill"},
	})
	proj := setupProject(t)
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	testRegistrySources = []registry.Source{{Name: "test", URL: "file://" + idx}}
	testPrompt = capability.AutoYes
	defer func() { testRegistrySources = nil; testPrompt = nil }()
	rootCmd.SetArgs([]string{"install", "go-guide", "--allow-unsigned"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, banned := range []string{".claude", "claude.md"} {
		if _, err := os.Stat(filepath.Join(proj, banned)); err == nil {
			t.Errorf("agnostic invariant violation: %s present after install", banned)
		}
	}
}

// silence unused import while iterating.
var _ = oci.Engine(nil)
