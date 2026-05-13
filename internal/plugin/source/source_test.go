package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initFixtureRepo creates a local git repo with one commit and a
// single tag. Returns the clone URL (file:// + abs path). Disables GPG
// signing and force-signed-annotated-tags via -c so the fixture works
// regardless of the developer's global git config.
func initFixtureRepo(t *testing.T, tagName string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitOverrides := []string{
		"-c", "commit.gpgsign=false",
		"-c", "tag.gpgsign=false",
		"-c", "tag.forceSignAnnotated=false",
	}
	runGit := func(args ...string) {
		c := exec.Command("git", append(gitOverrides, args...)...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "--initial-branch=main")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "README.md")
	runGit("commit", "-m", "initial")
	runGit("tag", tagName)
	return "file://" + dir
}

func TestFetchGit_VPrefixFallback(t *testing.T) {
	// Repo tags releases as v1.0.0; registry advertises 1.0.0.
	// Without the fallback, the install fails. With the fallback, it
	// retries with v1.0.0 and succeeds.
	cloneURL := initFixtureRepo(t, "v1.0.0")
	req := FetchRequest{
		Repo: cloneURL,
		Ref: "1.0.0",
	}
	got, err := fetchGit(context.Background(), req, cloneURL)
	if err != nil {
		t.Fatalf("fetchGit: %v", err)
	}
	defer got.Cleanup()
	if _, err := os.Stat(filepath.Join(got.Root, "README.md")); err != nil {
		t.Errorf("clone missing README.md: %v", err)
	}
}

func TestFetchGit_ExactRefStillWorks(t *testing.T) {
	// Repo tags as v1.0.0 and request asks for v1.0.0 verbatim. First
	// attempt succeeds; no fallback needed.
	cloneURL := initFixtureRepo(t, "v1.0.0")
	req := FetchRequest{
		Repo: cloneURL,
		Ref: "v1.0.0",
	}
	got, err := fetchGit(context.Background(), req, cloneURL)
	if err != nil {
		t.Fatalf("fetchGit: %v", err)
	}
	defer got.Cleanup()
}

func TestFetchGit_RefNotFoundEvenAfterFallback(t *testing.T) {
	// Repo only has v1.0.0; asking for 9.9.9 should fail after the
	// v9.9.9 retry also fails.
	cloneURL := initFixtureRepo(t, "v1.0.0")
	req := FetchRequest{
		Repo: cloneURL,
		Ref: "9.9.9",
	}
	_, err := fetchGit(context.Background(), req, cloneURL)
	if err == nil {
		t.Fatalf("expected clone failure for missing ref")
	}
}

func TestFetchGit_StripsDotGitFromCheckout(t *testing.T) {
	// Regression: rc.7 left the cloned plugin's .git/ tree under
	// `.samuel/plugins/<name>/`, bloating installs (~68% of an
	// actix-web install was git plumbing) and creating a nested git
	// repo inside the host project. Plugin resolution uses the
	// lockfile digest, not commit history, so .git/ has no downstream
	// consumer (issue #1).
	cloneURL := initFixtureRepo(t, "v1.0.0")
	req := FetchRequest{
		Repo: cloneURL,
		Ref:  "v1.0.0",
	}
	got, err := fetchGit(context.Background(), req, cloneURL)
	if err != nil {
		t.Fatalf("fetchGit: %v", err)
	}
	defer got.Cleanup()

	if _, err := os.Stat(filepath.Join(got.Root, "README.md")); err != nil {
		t.Errorf("expected README.md to survive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got.Root, ".git")); !os.IsNotExist(err) {
		t.Errorf(".git directory must not exist after fetch; err=%v", err)
	}
}

func TestVPrefixedSemver(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"1.0.0":          "v1.0.0",
		"1.4.2-rc.1":     "v1.4.2-rc.1",
		"v1.0.0":         "",  // already prefixed
		"V1.0.0":         "",  // case-insensitive guard
		"main":           "",  // not a digit-led ref
		"feature/x":      "",  // branch name
		"1":              "",  // no dot, ambiguous
		"abc.def":        "",  // not digit-led
		"2025-05-13":     "",  // date-style refs aren't semver — no fallback
	}
	for in, want := range cases {
		if got := vPrefixedSemver(in); got != want {
			t.Errorf("vPrefixedSemver(%q) = %q, want %q", in, got, want)
		}
	}
}
