//go:build e2e_live

package live

import (
	"path/filepath"
	"strings"
	"testing"
)

// Install block — CLI-surface coverage for the git-fetcher codepath
// (`source.fetchGit`) that the hermetic tier (file://) cannot exercise.
// Every test here installs from the public samuel-test-registry against
// real GitHub remotes.

// TestInstall_VPrefixedTag_Fetches asserts the rc.6 protection: when
// the registry publishes `latest = "1.0.0"` and the repo tags the
// release as `v1.0.0`, the v-prefix fallback in source.fetchGit retries
// and succeeds. Pre-rc.6 this failed with "remote branch 1.0.0 not
// found."
func TestInstall_VPrefixedTag_Fetches(t *testing.T) {
	p := withLiveRegistry(t, nil)

	var out string
	if err := retryOnce(t, func() error {
		var execErr error
		out, execErr = p.samuel("install", "samuel-test-skill-tagged-v@1.0.0")
		return execErr
	}); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	assertContains(t, out, "Installed samuel-test-skill-tagged-v@1.0.0", "v-prefix fixture must install at 1.0.0")
	if !p.fileExists(filepath.Join(".samuel", "plugins", "samuel-test-skill-tagged-v", "SKILL.md")) {
		t.Error("SKILL.md missing from installed plugin tree")
	}
}

// TestInstall_StripsDotGit asserts the rc.9 protection: the cloned
// `.git/` directory MUST be absent from the installed plugin tree.
// Samuel resolves plugins by name + lockfile digest; leaving `.git/`
// in place inflates install size and creates a nested git repo inside
// the host project.
func TestInstall_StripsDotGit(t *testing.T) {
	p := withLiveRegistry(t, nil)

	var out string
	if err := retryOnce(t, func() error {
		var execErr error
		out, execErr = p.samuel("install", "samuel-test-skill-with-git")
		return execErr
	}); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	assertContains(t, out, "Installed samuel-test-skill-with-git", "install must succeed")

	dotGit := filepath.Join(".samuel", "plugins", "samuel-test-skill-with-git", ".git")
	if p.fileExists(dotGit) {
		t.Errorf("rc.9 regression: .git/ leaked into installed plugin tree at %s", dotGit)
	}
}

// TestInstall_RegistryIndexParses is the happy-path test: end-to-end
// resolve → fetch → install lands the plugin and records it in
// samuel.lock with version + digest. Catches drift in the index.toml
// schema, the parser, and the lockfile writer at the same time.
func TestInstall_RegistryIndexParses(t *testing.T) {
	p := withLiveRegistry(t, nil)

	var out string
	if err := retryOnce(t, func() error {
		var execErr error
		out, execErr = p.samuel("install", "samuel-test-skill-basic")
		return execErr
	}); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	assertContains(t, out, "Installed samuel-test-skill-basic@1.0.0", "happy-path install must succeed")

	if !p.fileExists("samuel.lock") {
		t.Fatal("samuel.lock not written")
	}
	lock := p.readFile("samuel.lock")
	// go-toml/v2 renders strings with single quotes by default
	// (literal strings). The contract is the value, not the quote
	// style — check both forms so the test stays stable if the
	// serializer ever switches.
	if !containsAny(lock, `name = "samuel-test-skill-basic"`, `name = 'samuel-test-skill-basic'`) {
		t.Errorf("samuel.lock missing plugin entry:\n%s", lock)
	}
	if !containsAny(lock, `version = "1.0.0"`, `version = '1.0.0'`) {
		t.Errorf("samuel.lock missing version entry:\n%s", lock)
	}
	// `source` ties the lockfile entry to the registry repo and
	// proves the resolver picked the live fixture (not a cached
	// stale entry). `digest` is omitted for skill installs in v2.0
	// (`LockedPlugin.Digest` carries `toml:"digest,omitempty"`);
	// reintroduce the assertion once the WASM/OCI tiers populate
	// it for skill installs too.
	if !containsAny(lock,
		`source = "github.com/samuelpkg/samuel-test-skill-basic"`,
		`source = 'github.com/samuelpkg/samuel-test-skill-basic'`) {
		t.Errorf("samuel.lock missing source field:\n%s", lock)
	}
}

// TestInstall_UnknownPlugin_StructuredError asserts that asking for a
// plugin not in the registry fails with an actionable error rather
// than a crash or silent failure. The minimum bar: non-zero exit + the
// rendered message identifies the missing plugin by name. The PRD
// target is a fully structured error with DocsURL; until NotFoundError
// is upgraded to *errors.Error, this test guards the looser contract.
func TestInstall_UnknownPlugin_StructuredError(t *testing.T) {
	p := withLiveRegistry(t, nil)

	out, err := p.samuel("install", "samuel-test-skill-does-not-exist-anywhere")
	if err == nil {
		t.Fatalf("expected non-zero exit for unknown plugin; got success:\n%s", out)
	}
	assertContains(t, out, "samuel-test-skill-does-not-exist-anywhere", "error must name the missing plugin")
	// Either the structured `Error:` prefix (production renderer) or
	// the bare `registry: plugin not found:` string from NotFoundError
	// is acceptable; both are stable signals to the user.
	if !strings.Contains(out, "Error:") && !strings.Contains(out, "plugin not found") {
		t.Errorf("error message missing both 'Error:' prefix and 'plugin not found' marker:\n%s", out)
	}
}
