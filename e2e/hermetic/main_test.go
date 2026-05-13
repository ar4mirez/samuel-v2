//go:build e2e

// Package hermetic is the deterministic end-to-end test suite for the
// samuel CLI. Each test sets up a tempdir + a local file:// registry,
// runs the actual `samuel` binary, and asserts on stdout, stderr, exit
// code, and filesystem state.
//
// The suite intentionally does NOT touch the network or the live
// samuel-registry: hermetic = reproducible. A sibling `e2e/live`
// suite (build tag `e2e_live`) exercises the same flows against the
// real registry on a nightly cadence — that catches upstream drift
// that this suite by design cannot.
//
// Run locally:
//
//	go test -tags=e2e ./e2e/hermetic/...
//
// CI runs this on every PR; per-PR signal stays fast (<30s on a
// reasonable laptop) because the binary is built once per `go test`
// invocation in TestMain.
package hermetic

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// samuelBin is the absolute path to the built samuel binary, shared by
// every test in the package. TestMain builds it once.
var samuelBin string

// repoRoot is the absolute path to the samuel_v2 module root, computed
// once in TestMain from `go env GOMOD`.
var repoRoot string

func TestMain(m *testing.M) {
	root, err := goModRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e/hermetic: cannot find module root: %v\n", err)
		os.Exit(1)
	}
	repoRoot = root

	tmpDir, err := os.MkdirTemp("", "samuel-e2e-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e/hermetic: cannot create tempdir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	samuelBin = filepath.Join(tmpDir, "samuel")
	build := exec.Command("go", "build", "-o", samuelBin, "./cmd/samuel")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e/hermetic: build failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// goModRoot returns the directory containing the active go.mod file.
// Used so e2e tests can build the binary regardless of which subdir
// `go test` was invoked from.
func goModRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	goMod := string(out)
	// Trim trailing newline.
	if n := len(goMod); n > 0 && goMod[n-1] == '\n' {
		goMod = goMod[:n-1]
	}
	if goMod == "" || goMod == "/dev/null" {
		return "", fmt.Errorf("go env GOMOD returned empty")
	}
	return filepath.Dir(goMod), nil
}
