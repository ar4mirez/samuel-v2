//go:build e2e_live

package live

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// project is the handle a test holds for one initialized Samuel project
// pointed at the live test registry. Mirrors e2e/hermetic's project
// shape so behavior parity is obvious — the only intentional difference
// is `regURL` carries an HTTPS URL the network must reach instead of
// file://.
type project struct {
	t       *testing.T
	dir     string // absolute path to the project root
	home    string // HOME for this test (forces hermetic ~/.samuel/)
	regURL  string // live registry URL (github.com/... or https://...)
	regName string // registry name in samuel.toml
}

// withLiveRegistry materializes a tempdir + isolated HOME, runs
// `samuel init`, rewrites samuel.toml to point [[registries]] at the
// live URL, and exports SAMUEL_VERIFY_ALLOW_UNSIGNED=1 for the test's
// `samuel` invocations (the live fixtures are not signed yet — PRD 0008
// wires real sigstore).
//
// `configureFn`, if non-nil, runs after the standard wiring so a test
// can layer extra config (alternate registry name, additional sources,
// etc.) without forking the helper.
func withLiveRegistry(t *testing.T, configureFn func(p *project)) *project {
	t.Helper()
	p := newProject(t)
	p.pointAtLiveRegistry()
	if configureFn != nil {
		configureFn(p)
	}
	return p
}

// withAllowUnsigned exports SAMUEL_VERIFY_ALLOW_UNSIGNED=1 for this
// test's process environment until PRD 0008 lands real Sigstore
// verification of the fixture plugins. Cleanup restores the previous
// value on test end.
//
// Live tests should call this when they want the global env to apply
// to subprocesses they spawn directly (rare). For the common case of
// running `samuel` through (*project).samuel, the helper passes the
// env var on every invocation regardless.
func withAllowUnsigned(t *testing.T) {
	t.Helper()
	prev, had := os.LookupEnv("SAMUEL_VERIFY_ALLOW_UNSIGNED")
	if err := os.Setenv("SAMUEL_VERIFY_ALLOW_UNSIGNED", "1"); err != nil {
		t.Fatalf("setenv SAMUEL_VERIFY_ALLOW_UNSIGNED: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("SAMUEL_VERIFY_ALLOW_UNSIGNED", prev)
		} else {
			_ = os.Unsetenv("SAMUEL_VERIFY_ALLOW_UNSIGNED")
		}
	})
}

// newProject creates a fresh tempdir + HOME and runs `samuel init`.
// The project's samuel.toml is left with default registries — call
// pointAtLiveRegistry or withLiveRegistry to repoint it.
func newProject(t *testing.T) *project {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir()
	p := &project{t: t, dir: dir, home: home}

	out, err := p.samuel("init", ".", "--yes", "--minimal")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	return p
}

// samuel runs the built binary inside the project dir with HOME pinned
// to the test's hermetic home. Returns combined stdout+stderr and any
// error from the process. SAMUEL_VERIFY_ALLOW_UNSIGNED=1 is always set
// so the unsigned-fixtures contract is consistent across tests.
func (p *project) samuel(args ...string) (string, error) {
	return p.samuelWithEnv(nil, args...)
}

// samuelWithEnv is the variant used by the verify-live tests: callers
// can append extra env vars (last-write-wins, matching exec.Cmd) to
// override the default SAMUEL_VERIFY_ALLOW_UNSIGNED=1 baseline. The
// production sigstore verifier tier sets SAMUEL_VERIFY_ALLOW_UNSIGNED=0
// to force the cryptographic path on signed fixtures.
func (p *project) samuelWithEnv(extraEnv []string, args ...string) (string, error) {
	p.t.Helper()
	cmd := exec.Command(samuelBin, args...)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(),
		"HOME="+p.home,
		"XDG_CACHE_HOME="+filepath.Join(p.home, ".cache"),
		"SAMUEL_VERIFY_ALLOW_UNSIGNED=1",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// mustSamuel is samuel() with a t.Fatalf on non-zero exit. Use for
// setup; assertions should call samuel() directly so they can examine
// the error themselves.
func (p *project) mustSamuel(args ...string) string {
	p.t.Helper()
	out, err := p.samuel(args...)
	if err != nil {
		p.t.Fatalf("samuel %v: %v\n%s", args, err, out)
	}
	return out
}

// readFile is a t.Fatalf-on-error os.ReadFile shim.
func (p *project) readFile(rel string) string {
	p.t.Helper()
	body, err := os.ReadFile(filepath.Join(p.dir, rel))
	if err != nil {
		p.t.Fatalf("read %s: %v", rel, err)
	}
	return string(body)
}

// fileExists reports whether rel exists under the project dir.
func (p *project) fileExists(rel string) bool {
	p.t.Helper()
	_, err := os.Stat(filepath.Join(p.dir, rel))
	return err == nil
}

// pointAtLiveRegistry rewrites samuel.toml so the only configured
// registry source is the live test registry. We replace rather than
// merge because the default sources (github.com/samuelpkg/samuel) carry
// real plugins that would steal name resolution from the fixtures.
func (p *project) pointAtLiveRegistry() {
	p.t.Helper()
	p.regURL = liveRegistryURL
	p.regName = "samuel-test-registry"

	tomlPath := filepath.Join(p.dir, "samuel.toml")
	body := fmt.Sprintf(`version = "1"
default_methodology = "ralph"

[methodology.ralph]
  enabled = true
  agent = "claude"
  max_iterations = 25

[guardrails]
  max_function_lines = 50
  max_file_lines = 300
  require_tests = true

[[registries]]
  name = %q
  url = %q
  default = true

[translators.claude]
  enabled = true
`, p.regName, p.regURL)
	if err := os.WriteFile(tomlPath, []byte(body), 0o644); err != nil {
		p.t.Fatalf("rewrite samuel.toml: %v", err)
	}
}

// retryOnce runs fn; on failure, sleeps briefly and runs it once more.
// Returns the final error. Used to absorb transient network flakes on
// the nightly run — anything that fails twice in a row is a real
// regression, not noise.
//
// Capped at one retry to keep the wall-time budget; bugs that need
// retry > 1 to surface belong in the hermetic tier where we can make
// them deterministic.
func retryOnce(t *testing.T, fn func() error) error {
	t.Helper()
	if err := fn(); err == nil {
		return nil
	} else {
		t.Logf("first attempt failed (will retry once): %v", err)
		time.Sleep(2 * time.Second)
		if err2 := fn(); err2 != nil {
			return fmt.Errorf("retry also failed: first=%v second=%v", err, err2)
		}
		return nil
	}
}

// assertContains is the same single-string assert the hermetic tier
// uses. Kept duplicate here so both packages stay terse and don't
// import each other.
func assertContains(t *testing.T, got, want, why string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: expected output to contain %q\n----- got -----\n%s\n----- end -----", why, want, got)
	}
}

func assertNotContains(t *testing.T, got, want, why string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Errorf("%s: expected output to NOT contain %q\n----- got -----\n%s\n----- end -----", why, want, got)
	}
}

// containsAny reports whether haystack contains any of the candidate
// substrings. Used in lockfile assertions because go-toml/v2 may emit
// strings as either literal (single-quoted) or basic (double-quoted)
// — the value is what matters, not the quote style.
func containsAny(haystack string, candidates ...string) bool {
	for _, c := range candidates {
		if strings.Contains(haystack, c) {
			return true
		}
	}
	return false
}
