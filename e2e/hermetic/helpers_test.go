//go:build e2e

package hermetic

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// project is the handle a test holds for one initialized Samuel project.
// All helpers route through it so tests can stay terse.
type project struct {
	t       *testing.T
	dir     string // absolute path to the project root
	home    string // HOME for this test (forces hermetic ~/.samuel/)
	regURL  string // file:// URL of the test registry's index.toml (if any)
	regName string // name of the test registry (matches the source in samuel.toml)
}

// newProject creates a fresh tempdir + HOME, runs `samuel init` against
// it, and returns the project handle. Stdout/stderr from init is
// captured and reported on test failure.
//
// Each test gets its own HOME so they don't share `~/.samuel/cache/`
// or builtins state. Parallel-safe.
func newProject(t *testing.T) *project {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir()
	p := &project{t: t, dir: dir, home: home}

	out, err := p.samuel("init", ".", "--yes", "--minimal")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	return p
}

// samuel runs the built binary inside the project dir with HOME pinned
// to the test's hermetic home. Returns combined stdout+stderr and any
// error from the process. `samuel` exiting non-zero is reflected in
// the error (no implicit fatal — tests assert on error themselves).
func (p *project) samuel(args ...string) (string, error) {
	p.t.Helper()
	cmd := exec.Command(samuelBin, args...)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(),
		"HOME="+p.home,
		// Force a fresh registry cache per test home, so cached
		// index.toml from prior tests can't leak in.
		"XDG_CACHE_HOME="+filepath.Join(p.home, ".cache"),
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// mustSamuel is samuel() with a t.Fatalf on non-zero exit. Use when the
// command is part of test setup, not the assertion target.
func (p *project) mustSamuel(args ...string) string {
	p.t.Helper()
	out, err := p.samuel(args...)
	if err != nil {
		p.t.Fatalf("samuel %v: %v\n%s", args, err, out)
	}
	return out
}

// readFile is a t.Fatalf-on-error os.ReadFile shim. Returns content as
// a string because almost everything in this suite is text.
func (p *project) readFile(rel string) string {
	p.t.Helper()
	body, err := os.ReadFile(filepath.Join(p.dir, rel))
	if err != nil {
		p.t.Fatalf("read %s: %v", rel, err)
	}
	return string(body)
}

// fileExists returns true iff rel exists under the project dir.
func (p *project) fileExists(rel string) bool {
	p.t.Helper()
	_, err := os.Stat(filepath.Join(p.dir, rel))
	return err == nil
}

// writeFile creates or overwrites a file under the project dir.
func (p *project) writeFile(rel, content string) {
	p.t.Helper()
	full := filepath.Join(p.dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		p.t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		p.t.Fatalf("write %s: %v", rel, err)
	}
}

// rmFile removes a file under the project dir; non-fatal if absent.
func (p *project) rmFile(rel string) {
	p.t.Helper()
	_ = os.Remove(filepath.Join(p.dir, rel))
}

// setupRegistry materializes a plain skill source from the shared
// testdata/sample-skill/ fixture, writes a file:// index.toml pointing
// at it, and rewrites the project's samuel.toml to use the new
// registry as its only source.
//
// Registry name is "local" so the default policy's `allow_unsigned_for`
// list applies — installs work without --allow-unsigned while still
// exercising the policy-check path.
//
// IMPORTANT: file:// URLs route through `source.fetchFile`, NOT
// `source.fetchGit`. That means this harness cannot exercise the
// git-clone-specific behavior shipped in rc.6 (v-prefix tag fallback)
// or rc.9 (strip cloned .git/). Those regressions are covered by
// internal/plugin/source/source_test.go's TestFetchGit_* against a
// real local git repo. Validating them at the CLI surface requires
// the e2e/live tier.
func (p *project) setupRegistry(pluginName, version string) {
	p.t.Helper()
	regRoot := p.t.TempDir()
	srcRepo := p.skillSource(pluginName, version, regRoot)

	indexPath := filepath.Join(regRoot, "index.toml")
	indexBody := fmt.Sprintf(`schema_version = 1

[[plugins]]
name = %q
repo = "file://%s"
latest = %q
description = "hermetic e2e fixture"
categories = ["test"]
tags = ["fixture"]
kind = "skill"
`, pluginName, srcRepo, version)
	if err := os.WriteFile(indexPath, []byte(indexBody), 0o644); err != nil {
		p.t.Fatalf("write index.toml: %v", err)
	}

	p.regURL = "file://" + indexPath
	p.regName = "local"

	// Replace the registries block in samuel.toml. Simpler than parsing:
	// rewrite the file with a fresh default-ish body that points at us.
	tomlPath := filepath.Join(p.dir, "samuel.toml")
	body := fmt.Sprintf(`version = "1"
default_methodology = "ralph"

[methodology.ralph]
  enabled = true
  agent = "claude"
  max_iterations = 25

[guardrails]
  max_function_lines = 50
  max_file_lines = 300
  require_tests = true

[[registries]]
  name = "local"
  url = %q
  default = true

[translators.claude]
  enabled = true
`, p.regURL)
	if err := os.WriteFile(tomlPath, []byte(body), 0o644); err != nil {
		p.t.Fatalf("rewrite samuel.toml: %v", err)
	}
}

// skillSource copies testdata/sample-skill into workdir/<name>-src/
// and rewrites the manifest with the requested name + version. No
// git involvement — file:// URLs go through `source.fetchFile` which
// just returns the directory as-is, so any `.git/` here would leak
// straight into the install.
func (p *project) skillSource(name, version, workdir string) string {
	p.t.Helper()
	src := filepath.Join(workdir, name+"-src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		p.t.Fatalf("mkdir src: %v", err)
	}
	fixture := filepath.Join(testdataDir(p.t), "sample-skill")
	if err := copyTree(fixture, src); err != nil {
		p.t.Fatalf("copy fixture: %v", err)
	}
	manifest := fmt.Sprintf(`name = %q
version = %q
kind = "skill"
summary = "Hermetic-e2e fixture skill"

[capabilities]
required = ["filesystem.read"]
`, name, version)
	if err := os.WriteFile(filepath.Join(src, "samuel-plugin.toml"), []byte(manifest), 0o644); err != nil {
		p.t.Fatalf("rewrite manifest: %v", err)
	}
	return src
}

// testdataDir returns the absolute path to e2e/hermetic/testdata for
// the running test binary. runtime.Caller(0) anchors at this helpers
// file regardless of where the test was invoked from.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "testdata")
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		in, openErr := os.Open(p)
		if openErr != nil {
			return openErr
		}
		defer in.Close()
		w, createErr := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if createErr != nil {
			return createErr
		}
		defer w.Close()
		_, err = io.Copy(w, in)
		return err
	})
}

// assertContains is a t.Errorf shim with a clearer failure message
// than strings.Contains in a raw bool assertion.
func assertContains(t *testing.T, got, want, why string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: expected output to contain %q\n----- got -----\n%s\n----- end -----", why, want, got)
	}
}

func assertNotContains(t *testing.T, got, want, why string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Errorf("%s: expected output to NOT contain %q\n----- got -----\n%s\n----- end -----", why, want, got)
	}
}

// silence the unused-import warning when only a subset of helpers are
// used in a given file; bytes.Buffer is reserved for future tests.
var _ = bytes.NewBuffer
