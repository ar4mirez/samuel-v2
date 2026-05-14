//go:build e2e_live

package live

import "testing"

// Search block — proves the registry index loads + the search ranker
// returns the fixture plugin for a known-good keyword.

func TestSearch_FindsByKeyword(t *testing.T) {
	p := withLiveRegistry(t, nil)

	var out string
	if err := retryOnce(t, func() error {
		var execErr error
		out, execErr = p.samuel("search", "samuel-e2e-live")
		return execErr
	}); err != nil {
		t.Fatalf("search: %v\n%s", err, out)
	}
	// The tag `samuel-e2e-live` is shared by every fixture plugin;
	// search must surface at least one match.
	assertContains(t, out, "samuel-test-skill-basic", "search must surface fixture plugin")
}
