//go:build e2e_live

package live

import "testing"

// Update block — exercises the multi-version registry path against
// real git tags. The updatable fixture publishes 1.0.0 and 1.1.0; the
// test installs the older version, runs `samuel update`, and asserts
// the lockfile reflects the bump.

func TestUpdate_LiveRegistry_BumpsVersion(t *testing.T) {
	p := withLiveRegistry(t, nil)

	// Install at the older version.
	var installOut string
	if err := retryOnce(t, func() error {
		var execErr error
		installOut, execErr = p.samuel("install", "samuel-test-skill-updatable@1.0.0")
		return execErr
	}); err != nil {
		t.Fatalf("install: %v\n%s", err, installOut)
	}
	assertContains(t, installOut, "Installed samuel-test-skill-updatable@1.0.0", "install must pin to 1.0.0")

	// Update to the latest (1.1.0 per the registry index).
	var updateOut string
	if err := retryOnce(t, func() error {
		var execErr error
		updateOut, execErr = p.samuel("update", "samuel-test-skill-updatable")
		return execErr
	}); err != nil {
		t.Fatalf("update: %v\n%s", err, updateOut)
	}
	assertContains(t, updateOut, "samuel-test-skill-updatable -> 1.1.0", "update must surface the 1.0.0 → 1.1.0 transition")

	lock := p.readFile("samuel.lock")
	if !containsAny(lock, `version = "1.1.0"`, `version = '1.1.0'`) {
		t.Errorf("samuel.lock did not bump to 1.1.0 after update:\n%s", lock)
	}
}
