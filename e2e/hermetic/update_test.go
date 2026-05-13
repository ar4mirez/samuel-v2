//go:build e2e

package hermetic

import "testing"

// Update block — covers rc.7 (signature flag parity + cache key
// includes AllowUnsigned + update success line carries reason).

func TestUpdate_NoFlagPrintsVerificationReason(t *testing.T) {
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")

	out := p.mustSamuel("update", "sample-skill")
	// rc.7 changed the update success line to carry the verify reason.
	assertContains(t, out, "sample-skill -> 1.0.0", "update must succeed")
	assertContains(t, out, "verified", "update must surface verification status")
}

func TestUpdate_AllowUnsignedReasonDiffersFromDefault(t *testing.T) {
	// rc.7 regression: the verifier cache used to key on file digest
	// only, so toggling --allow-unsigned would return a stale prior
	// decision. The cache key now includes AllowUnsigned, so the two
	// reasons MUST differ.
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")

	noFlag := p.mustSamuel("update", "sample-skill")
	withFlag := p.mustSamuel("update", "sample-skill", "--allow-unsigned")

	// Both must contain "verified" but the parenthetical reason must
	// differ. Extracting "(...)" exactly is brittle, so assert on the
	// presence of the --allow-unsigned token in only the second run.
	assertNotContains(t, noFlag, "(--allow-unsigned)", "no-flag run must not show --allow-unsigned reason")
	assertContains(t, withFlag, "(--allow-unsigned)", "--allow-unsigned run must show that reason")
}

func TestUpdate_AcceptsInstallFlags(t *testing.T) {
	// rc.7 also added flag parity — update must accept the same
	// signature/policy flags as install. Pre-rc.7 they were rejected
	// as "unknown flag".
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")

	// Should not error with "unknown flag: --allow-unsigned".
	if _, err := p.samuel("update", "sample-skill", "--allow-unsigned"); err != nil {
		t.Errorf("update --allow-unsigned should be accepted: %v", err)
	}
	if _, err := p.samuel("update", "sample-skill", "--dry-run"); err != nil {
		t.Errorf("update --dry-run should be accepted: %v", err)
	}
}

func TestUpdate_BulkAllUpdatesEveryInstalledPlugin(t *testing.T) {
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")

	out := p.mustSamuel("update", "--all")
	assertContains(t, out, "sample-skill -> 1.0.0", "update --all must touch every installed plugin")
}
