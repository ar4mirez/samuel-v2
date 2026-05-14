//go:build e2e_live

package live

import (
	"os"
	"strings"
	"testing"
)

// verifyFixturesAvailable reports whether the signed / unsigned /
// wrong-identity test fixtures have been published. The test-registry
// shipped without them (PRD 0008's sign-fixtures.yml workflow was
// scoped but never built), so the four TestVerify_* tests skip by
// default. Flip SAMUEL_LIVE_VERIFY_FIXTURES=1 once
// samuel-test-skill-signed, samuel-test-skill-unsigned, and
// samuel-test-skill-wrong-identity exist as standalone repos under
// samuelpkg, are registered in samuel-test-registry, and (for the
// signed one) carry a cosign --new-bundle-format sigstore bundle.
func verifyFixturesAvailable() bool {
	return os.Getenv("SAMUEL_LIVE_VERIFY_FIXTURES") == "1"
}

// Verify block — PRD 0008 live coverage of the sigstore-go verifier.
//
// The signed fixtures are released by samuel-test-registry's
// sign-fixtures.yml workflow (keyless cosign + OIDC). Tests here drive
// the actual `samuel` binary against the public registry; the
// production verifier fetches the TUF trust root from
// tuf-repo-cdn.sigstore.dev on first call and looks up each artifact's
// signature in Rekor.
//
// Wall-time cost matters: each verify call is the cold path until the
// trust root is cached in the test's HOME. The test helper points the
// HOME at a tempdir, so each test starts with a fresh cache —
// individual tests run in ~3-5s. The full suite stays inside the
// e2e/live WallTimeBudget of 2 minutes.

// TestVerify_SignedFixture_Verifies installs the cosign-signed fixture
// and asserts the production verifier reports `Verified: true` plus
// surfaces the signing identity in the install line. The fixture's
// OIDC subject must match the default identity_patterns
// (`https://github.com/samuelpkg/*`).
func TestVerify_SignedFixture_Verifies(t *testing.T) {
	if !verifyFixturesAvailable() {
		t.Skip("samuel-test-skill-signed not yet in registry; set SAMUEL_LIVE_VERIFY_FIXTURES=1 once the cosign-signed fixture repo exists")
	}
	p := newProject(t)
	p.pointAtLiveRegistry()
	// NOTE: explicitly DO NOT use withAllowUnsigned — we want the
	// production sigstore path to gate the install.
	out := mustInstallReal(t, p, "samuel-test-skill-signed")

	// "signed by" line surfaces the actual OIDC identity. We don't
	// pin the exact URL because the workflow tag and run-id can
	// drift; the substring is the contract.
	assertContains(t, out, "Installed samuel-test-skill-signed@1.0.0", "signed install must succeed")
	if !strings.Contains(out, "signed by") && !strings.Contains(out, "signature: verified") {
		t.Errorf("expected install line to surface signing identity or verified state:\n%s", out)
	}
}

// TestVerify_UnsignedFixture_RejectsWithoutFlag asserts that the
// unsigned fixture fails-closed under the production policy. The error
// is structured and carries a DocsURL pointing at the signing concepts
// page.
func TestVerify_UnsignedFixture_RejectsWithoutFlag(t *testing.T) {
	if !verifyFixturesAvailable() {
		t.Skip("samuel-test-skill-unsigned not yet in registry; set SAMUEL_LIVE_VERIFY_FIXTURES=1 once the unsigned fixture repo exists")
	}
	p := newProject(t)
	p.pointAtLiveRegistry()

	out, err := installNoUnsigned(p, "samuel-test-skill-unsigned")
	if err == nil {
		t.Fatalf("expected unsigned install to fail-closed; got success:\n%s", out)
	}
	// Either the structured "[plugin/verify]" prefix or the docs URL
	// is acceptable — both are stable signals to the user.
	if !strings.Contains(out, "plugin/verify") && !strings.Contains(out, "signature") {
		t.Errorf("error should mention verify subsystem:\n%s", out)
	}
	if !strings.Contains(out, "concepts/signing") && !strings.Contains(out, "Docs:") {
		t.Errorf("error should point at the docs URL:\n%s", out)
	}
}

// TestVerify_UnsignedFixture_AcceptsWithFlag asserts the
// --allow-unsigned escape hatch still works after the v2.1 flip; the
// lockfile records Reason=--allow-unsigned for audit.
func TestVerify_UnsignedFixture_AcceptsWithFlag(t *testing.T) {
	if !verifyFixturesAvailable() {
		t.Skip("samuel-test-skill-unsigned not yet in registry; set SAMUEL_LIVE_VERIFY_FIXTURES=1 once the unsigned fixture repo exists")
	}
	p := newProject(t)
	p.pointAtLiveRegistry()
	withAllowUnsigned(t)

	out, err := p.samuel("install", "samuel-test-skill-unsigned", "--allow-unsigned")
	if err != nil {
		t.Fatalf("install --allow-unsigned: %v\n%s", err, out)
	}
	assertContains(t, out, "Installed samuel-test-skill-unsigned", "--allow-unsigned install must succeed")
	if !strings.Contains(out, "--allow-unsigned") && !strings.Contains(out, "allow-unsigned") {
		t.Errorf("install output should surface --allow-unsigned reason:\n%s", out)
	}
}

// TestVerify_WrongIdentity_Rejects asserts that a real signature whose
// OIDC subject lies outside the default identity_patterns fails
// verification. The error must cite the identity-pattern mismatch so
// the user knows what to do.
func TestVerify_WrongIdentity_Rejects(t *testing.T) {
	if !verifyFixturesAvailable() {
		t.Skip("samuel-test-skill-wrong-identity not yet in registry; set SAMUEL_LIVE_VERIFY_FIXTURES=1 once the wrong-identity fixture repo exists")
	}
	p := newProject(t)
	p.pointAtLiveRegistry()

	out, err := installNoUnsigned(p, "samuel-test-skill-wrong-identity")
	if err == nil {
		t.Fatalf("expected wrong-identity install to fail; got success:\n%s", out)
	}
	// The error message should reference signature or identity to
	// make the failure mode actionable.
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "signature") && !strings.Contains(lower, "identity") {
		t.Errorf("wrong-identity error should mention signature or identity:\n%s", out)
	}
}

// mustInstallReal runs `samuel install` without the
// SAMUEL_VERIFY_ALLOW_UNSIGNED=1 default, so the production sigstore
// verifier gates the call. Retries once on transient network blip.
func mustInstallReal(t *testing.T, p *project, name string) string {
	t.Helper()
	var out string
	err := retryOnce(t, func() error {
		var execErr error
		out, execErr = installNoUnsigned(p, name)
		return execErr
	})
	if err != nil {
		t.Fatalf("install %s (production verifier): %v\n%s", name, err, out)
	}
	return out
}

// installNoUnsigned is a samuel() variant that strips
// SAMUEL_VERIFY_ALLOW_UNSIGNED from the env so the verify policy fires
// as it would in a real user's shell. Returns the same (stdout, error)
// shape as project.samuel.
func installNoUnsigned(p *project, name string) (string, error) {
	return samuelEnv(p, []string{"SAMUEL_VERIFY_ALLOW_UNSIGNED=0"}, "install", name)
}

// samuelEnv runs the samuel binary with an extra env-var slice merged
// over the default helper env. Last-write-wins inside the merged slice
// matches exec.Cmd's behavior, so callers can override
// SAMUEL_VERIFY_ALLOW_UNSIGNED here.
func samuelEnv(p *project, extraEnv []string, args ...string) (string, error) {
	out, err := p.samuelWithEnv(extraEnv, args...)
	return out, err
}
