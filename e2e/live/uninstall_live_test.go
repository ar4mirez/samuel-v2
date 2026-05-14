//go:build e2e_live

package live

import (
	"strings"
	"testing"
)

// Uninstall block — round-trip the fixture through install + uninstall
// and assert state ends fully clean (no entry in samuel.lock, no
// directory under .samuel/plugins/).

func TestUninstall_RemovesFromLockAndTree(t *testing.T) {
	p := withLiveRegistry(t, nil)

	var installOut string
	if err := retryOnce(t, func() error {
		var execErr error
		installOut, execErr = p.samuel("install", "samuel-test-skill-basic")
		return execErr
	}); err != nil {
		t.Fatalf("install: %v\n%s", err, installOut)
	}

	p.mustSamuel("uninstall", "samuel-test-skill-basic")

	if p.fileExists(".samuel/plugins/samuel-test-skill-basic") {
		t.Error("uninstall did not remove plugin dir")
	}
	if p.fileExists("samuel.lock") {
		lock := p.readFile("samuel.lock")
		if strings.Contains(lock, "samuel-test-skill-basic") {
			t.Errorf("samuel.lock still references plugin after uninstall:\n%s", lock)
		}
	}
	ls := p.mustSamuel("ls")
	assertContains(t, ls, "Installed plugins (0)", "ls must show empty after uninstall")
}
