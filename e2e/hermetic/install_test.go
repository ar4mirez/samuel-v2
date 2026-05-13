//go:build e2e

package hermetic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Install block — covers rc.3 (registry parser), rc.14 (--dry-run),
// and the general install pipeline (search → resolve → fetch →
// install → uninstall).
//
// LIMITATION: file:// URLs in the registry route through
// `source.fetchFile`, not `source.fetchGit`. That means this hermetic
// tier cannot exercise rc.6 (v-prefix tag fallback) or rc.9 (strip
// cloned .git/) at the CLI surface — both bugs live in the git
// fetcher. Those fixes are protected by unit tests in
// `internal/plugin/source/source_test.go` against a real local git
// repo. End-to-end coverage of them belongs in the e2e/live tier
// where the real samuel-registry pulls from real https:// repos.

func TestInstall_SearchFindsHermeticFixture(t *testing.T) {
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	out := p.mustSamuel("search", "sample")
	assertContains(t, out, "sample-skill", "search must surface the fixture plugin")
	assertContains(t, out, "1.0.0", "search must surface the fixture version")
}

func TestInstall_BasicPipeline(t *testing.T) {
	// End-to-end: registry resolves → fetch → install lands artifact
	// at .samuel/plugins/<name>/SKILL.md, samuel.toml gains the
	// [[plugins]] entry, samuel.lock records the install. The git
	// fetcher's v-prefix behavior (rc.6) is unit-tested separately;
	// here we just assert the user-facing success path works.
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	out := p.mustSamuel("install", "sample-skill")
	assertContains(t, out, "Installed sample-skill@1.0.0", "install must succeed")
	if !p.fileExists(".samuel/plugins/sample-skill/SKILL.md") {
		t.Error("SKILL.md missing from installed plugin tree")
	}
}

func TestInstall_NoStrayDotGit(t *testing.T) {
	// The install pipeline must not introduce a .git/ directory at
	// the plugin root, even when neither the source nor the fetcher
	// is git-aware. Catches accidental future regressions where some
	// new pipeline step copies the wrong tree shape. The
	// fetchGit-specific .git strip from rc.9 is unit-tested
	// separately against a real local git repo.
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")
	if p.fileExists(".samuel/plugins/sample-skill/.git") {
		t.Error(".git/ directory present in installed plugin tree")
	}
}

func TestInstall_FootprintMatchesPayload(t *testing.T) {
	// Sample-skill fixture has SKILL.md + samuel-plugin.toml. Anything
	// over a low single-digit file count means non-payload leakage.
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")

	pluginDir := filepath.Join(p.dir, ".samuel", "plugins", "sample-skill")
	count := 0
	err := filepath.Walk(pluginDir, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", pluginDir, err)
	}
	if count > 5 {
		t.Errorf("install footprint too large: %d files (expected ≤5 for fixture payload)", count)
	}
}

func TestInstall_VersionConstraintResolves(t *testing.T) {
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	out := p.mustSamuel("install", "sample-skill@^1.0.0")
	assertContains(t, out, "Installed sample-skill@1.0.0", "constraint @^1.0.0 must resolve to 1.0.0")
}

func TestInstall_UnsatisfiableConstraintErrors(t *testing.T) {
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	out, err := p.samuel("install", "sample-skill@^2.0.0")
	if err == nil {
		t.Fatal("expected non-zero exit for unsatisfiable constraint")
	}
	assertContains(t, out, "no version satisfies", "error must surface the constraint failure")
}

func TestInstall_DryRunDoesNotClaimInstalled(t *testing.T) {
	// rc.14 regression: --dry-run used to print `✓ Installed …`. Must
	// render `(dry-run) Would install …` instead, and side effects
	// must be suppressed.
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	out := p.mustSamuel("install", "sample-skill", "--dry-run")
	assertNotContains(t, out, "✓ Installed", "dry-run must not claim success")
	assertContains(t, out, "(dry-run)", "dry-run must mark itself")
	assertContains(t, out, "Would install sample-skill", "dry-run must preview the install")
	if p.fileExists(".samuel/plugins/sample-skill") {
		t.Error("dry-run wrote files; expected no side effects")
	}
}

func TestInstall_UninstallReversesState(t *testing.T) {
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")
	p.mustSamuel("uninstall", "sample-skill")

	if p.fileExists(".samuel/plugins/sample-skill") {
		t.Error("uninstall did not remove plugin dir")
	}
	ls := p.mustSamuel("ls")
	assertContains(t, ls, "Installed plugins (0)", "ls must show empty after uninstall")
	// samuel.toml [[plugins]] entry should be gone.
	toml := p.readFile("samuel.toml")
	if strings.Contains(toml, "sample-skill") {
		t.Errorf("samuel.toml still references sample-skill after uninstall:\n%s", toml)
	}
}
