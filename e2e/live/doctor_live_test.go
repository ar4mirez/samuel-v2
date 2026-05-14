//go:build e2e_live

package live

import (
	"strings"
	"testing"
)

// Doctor block — once a fixture plugin is installed via the live
// fetcher, `samuel doctor` must report it as healthy. Combined with
// the install-side tests, this catches drift between the install
// pipeline and the doctor check pipeline (they share `manifest.LoadFromDir`
// but have separate downstream checks).

func TestDoctor_LiveInstalledPlugin_HealthOK(t *testing.T) {
	p := withLiveRegistry(t, nil)

	var installOut string
	if err := retryOnce(t, func() error {
		var execErr error
		installOut, execErr = p.samuel("install", "samuel-test-skill-basic")
		return execErr
	}); err != nil {
		t.Fatalf("install: %v\n%s", err, installOut)
	}

	out := p.mustSamuel("doctor", "--json")
	// Machine consumers read --json; assert on the keys that matter.
	for _, key := range []string{
		`"schemaVersion"`,
		`"checks"`,
		`"summary"`,
	} {
		assertContains(t, out, key, "doctor --json envelope must include "+key)
	}
	// The live-installed plugin must appear as a check entry, and it
	// must report a passing status (no "✗" or `"status":"failed"`
	// against this name).
	if !strings.Contains(out, "samuel-test-skill-basic") {
		t.Errorf("doctor --json did not include the installed plugin check:\n%s", out)
	}
	// Quick negative on the failure marker — JSON renders failed checks
	// with `"status":"failed"`.
	failedMarker := `"status":"failed"`
	if strings.Contains(out, "samuel-test-skill-basic") &&
		strings.Contains(out, failedMarker) {
		// Tighter check: only fail if the plugin's own block carries the
		// failure marker. We look for the marker within a small window
		// after the plugin name to avoid flagging unrelated failed checks.
		idx := strings.Index(out, "samuel-test-skill-basic")
		end := idx + 256
		if end > len(out) {
			end = len(out)
		}
		if strings.Contains(out[idx:end], failedMarker) {
			t.Errorf("doctor reports plugin as failed:\n%s", out)
		}
	}
}
