package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	samerrors "github.com/samuelpkg/samuel/internal/errors"
)

// TestSigstoreVerifier_MissingBundleStructuredError asserts that when
// the policy demands a signature and the artifact has no .bundle
// sidecar, the verifier returns a structured *errors.Error pointing the
// user at the docs page — not a sigstore-go internal error string.
//
// We sidestep the TUF fetch by pointing the verifier at an empty
// trust-root cache directory; the missing-bundle branch fires before
// any cryptographic work happens.
func TestSigstoreVerifier_MissingBundleStructuredError(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact.tar")
	if err := os.WriteFile(artifact, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	v := NewSigstoreVerifier(DefaultPolicy(),
		WithTrustRootDir(""), // disable on-disk cache lookup
	)
	// Pre-seal the trust-root sync.Once with no material so verify
	// reaches the missing-bundle branch deterministically (no network).
	v.once.Do(func() { v.loadErr = nil })

	_, err := v.VerifyBlob(context.Background(), artifact, Request{
		Policy: DefaultPolicy(),
		Source: "github.com/samuelpkg/samuel-go-guide",
		Plugin: "test-plugin",
	})
	if err == nil {
		t.Fatal("expected missing-bundle error")
	}
	var sErr *samerrors.Error
	if !errors.As(err, &sErr) {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if sErr.DocsURL == "" {
		t.Errorf("structured error should carry DocsURL")
	}
	if !sErr.Recoverable {
		t.Errorf("structured error should be Recoverable")
	}
}

// TestSigstoreVerifier_TrustRootCacheKeySaltsOnVersion asserts the
// trust-root cache path embeds the samuel version, so a binary
// upgrade invalidates any prior cached trust root.
func TestSigstoreVerifier_TrustRootCacheKeySaltsOnVersion(t *testing.T) {
	dir := t.TempDir()
	v := NewSigstoreVerifier(DefaultPolicy(),
		WithTrustRootDir(dir),
		WithSamuelVersion("v2.1.0"),
	)
	path := v.trustRootCachePath()
	if path == "" {
		t.Fatal("expected non-empty cache path")
	}
	if !strings.Contains(path, "v2.1.0") {
		t.Errorf("cache path should include samuel version: %s", path)
	}

	v2 := NewSigstoreVerifier(DefaultPolicy(),
		WithTrustRootDir(dir),
		WithSamuelVersion("v2.1.1"),
	)
	if v.trustRootCachePath() == v2.trustRootCachePath() {
		t.Errorf("cache path should change across versions; both = %s", path)
	}
}

// TestSigstoreVerifier_TrustRootCacheTTL covers the staleness branch:
// a cached trust root older than tufTTL must be ignored. We write a
// dummy file, then call readCachedTrustRoot with a clock stub that
// reports "now" 48h ahead. The expired branch returns an error and
// avoids handing back a stale root.
func TestSigstoreVerifier_TrustRootCacheTTL(t *testing.T) {
	dir := t.TempDir()
	v := NewSigstoreVerifier(DefaultPolicy(),
		WithTrustRootDir(dir),
		WithSamuelVersion("v2.1.0"),
		WithClock(func() time.Time { return time.Now().Add(48 * time.Hour) }),
	)
	path := v.trustRootCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := v.readCachedTrustRoot(); err == nil {
		t.Errorf("expected stale-cache error, got nil")
	}
}

// TestGlobToRegex confirms the identity-pattern → regex conversion
// matches the same semantics as verify.globMatch (the original
// non-regex matcher). Both styles must accept the same identities.
func TestGlobToRegex(t *testing.T) {
	cases := []struct {
		pattern string
		input   string
		want    bool
	}{
		{"https://github.com/samuelpkg/*", "https://github.com/samuelpkg/samuel-go-guide", true},
		{"https://github.com/samuelpkg/*", "https://github.com/wrong/repo", false},
		{"https://github.com/foo/**", "https://github.com/foo/bar/baz", true},
		{"https://github.com/foo/bar", "https://github.com/foo/bar", true},
		{"https://github.com/foo/bar", "https://github.com/foo/baz", false},
	}
	for _, c := range cases {
		re, err := regexp.Compile(globToRegex(c.pattern))
		if err != nil {
			t.Fatalf("globToRegex(%q) produced invalid regex: %v", c.pattern, err)
		}
		if got := re.MatchString(c.input); got != c.want {
			t.Errorf("globToRegex(%q) on %q = %v, want %v (regex=%q)", c.pattern, c.input, got, c.want, re.String())
		}
	}
}

// TestSigstore_FixtureCorpusReadme documents the fixture-rotation
// playbook. It is a guard test: when sigstore-go ships a breaking
// trust-root format change, this test reminds the maintainer to
// re-record testdata/sigstore/ fixtures from upstream.
//
// See docs/concepts/signing.md "Fixture rotation" for the playbook.
func TestSigstore_FixtureCorpusReadme(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "sigstore")
	if _, err := os.Stat(fixtureDir); err != nil {
		t.Skipf("no fixtures under %s yet — see docs/concepts/signing.md rotation playbook", fixtureDir)
	}
	readme := filepath.Join(fixtureDir, "README.md")
	if _, err := os.Stat(readme); err != nil {
		t.Errorf("fixture corpus is missing %s — document the rotation playbook before merging", readme)
	}
}
