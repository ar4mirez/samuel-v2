package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/samuelpkg/samuel/internal/agents"
	"github.com/samuelpkg/samuel/internal/config"
	"github.com/samuelpkg/samuel/internal/lock"
)

func TestUpdateAgents_PinsEveryDefaultImage(t *testing.T) {
	defer pinTestEnv(t)()
	proj := setupProject(t)
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}

	// Stub the resolver so the test does not need podman/docker.
	testAgentResolver = func(image string) (string, error) {
		return "sha256:" + strings.Repeat("a", 64), nil
	}
	defer func() { testAgentResolver = nil }()

	ResetFlagsForTest()
	rootCmd.SetArgs([]string{"update", "--agents"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lf, err := lock.ReadLockfile(proj)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	registered := map[string]struct{}{}
	for _, name := range agents.List() {
		a, _ := agents.Get(name)
		if a.Manifest().DefaultImage != "" {
			registered[name] = struct{}{}
		}
	}
	if len(lf.Agents) != len(registered) {
		t.Errorf("want %d pinned agents, got %d: %+v", len(registered), len(lf.Agents), lf.Agents)
	}
	for _, la := range lf.Agents {
		if !strings.HasPrefix(la.Digest, "sha256:") {
			t.Errorf("%s digest not pinned: %+v", la.Adapter, la)
		}
	}
}

// _ keeps the config import in scope when the package is built without
// the test-only path above.
var _ config.LockedAgent
