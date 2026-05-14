//go:build e2e_live

// Package live is the network-allowed end-to-end test tier. Each test
// drives the actual `samuel` binary against the public
// github.com/samuelpkg/samuel-test-registry registry, exercising the
// `source.fetchGit` codepath that the hermetic tier (file://) cannot
// reach.
//
// The hermetic tier (e2e/hermetic, build tag `e2e`) is the PR gate.
// This tier is the *drift detector* — it runs nightly and on manual
// dispatch, and is allowed to fail without blocking merges. See the
// tier matrix in e2e/README.md and docs/reference/testing.md.
//
// Run locally:
//
//	go test -tags=e2e_live ./e2e/live/...
//
// Point at a different registry (mirror, fork, branch) with:
//
//	SAMUEL_LIVE_REGISTRY_URL=github.com/<you>/samuel-test-registry \
//	  go test -tags=e2e_live ./e2e/live/...
package live

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// WallTimeBudget is the hard ceiling for the full live suite. Per the
// PRD, going over means we have to refactor before adding coverage —
// the live tier is a drift detector, not a stress test, and a slow
// suite erodes the nightly's value as a punch list.
const WallTimeBudget = 2 * time.Minute

// samuelBin is the absolute path to the built samuel binary, shared by
// every test in the package. TestMain builds it once.
var samuelBin string

// repoRoot is the absolute path to the samuel module root, computed
// once in TestMain from `go env GOMOD`.
var repoRoot string

// liveRegistryURL is the registry source URL the live suite runs
// against. Default is the public samuelpkg/samuel-test-registry; override
// with SAMUEL_LIVE_REGISTRY_URL for forks, branches, or local mirrors.
var liveRegistryURL string

// DefaultLiveRegistry is the canonical public registry used by the
// nightly job. Tests targeting specific fixture plugins (rc.6, rc.9,
// updatable) assume this registry's contents. Overriding the URL is
// safe as long as the index.toml schema + plugin matrix matches
// samuel-test-registry/index.toml in this repo.
const DefaultLiveRegistry = "github.com/samuelpkg/samuel-test-registry"

func TestMain(m *testing.M) {
	root, err := goModRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e/live: cannot find module root: %v\n", err)
		os.Exit(1)
	}
	repoRoot = root

	liveRegistryURL = os.Getenv("SAMUEL_LIVE_REGISTRY_URL")
	if liveRegistryURL == "" {
		liveRegistryURL = DefaultLiveRegistry
	}

	tmpDir, err := os.MkdirTemp("", "samuel-e2e-live-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e/live: cannot create tempdir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	samuelBin = filepath.Join(tmpDir, "samuel")
	build := exec.Command("go", "build", "-o", samuelBin, "./cmd/samuel")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e/live: build failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	start := time.Now()
	code := m.Run()
	elapsed := time.Since(start)
	// Surface the suite wall-time on every run, pass or fail, so the
	// nightly CI log carries a self-documenting performance trend.
	fmt.Fprintf(os.Stderr, "e2e/live: suite wall-time %s (budget %s)\n", elapsed.Round(time.Millisecond), WallTimeBudget)
	if elapsed > WallTimeBudget {
		fmt.Fprintf(os.Stderr, "e2e/live: BUDGET EXCEEDED — refactor before adding coverage\n")
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// goModRoot returns the directory containing the active go.mod file.
func goModRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	goMod := string(out)
	if n := len(goMod); n > 0 && goMod[n-1] == '\n' {
		goMod = goMod[:n-1]
	}
	if goMod == "" || goMod == "/dev/null" {
		return "", fmt.Errorf("go env GOMOD returned empty")
	}
	return filepath.Dir(goMod), nil
}
