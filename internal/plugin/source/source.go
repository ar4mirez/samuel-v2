// Package source materializes a plugin's source tree (manifest + files)
// into a local directory the loaders can read from. Three transports:
//
//   - file://<abs-path>   — local fixture (tests + dev mode)
//   - https://...         — git clone over HTTPS via the user's `git` CLI
//   - github.com/owner/r  — shorthand for https://github.com/owner/r
//
// The Fetcher's contract is small: produce a directory containing the
// plugin tree at the requested ref, and return a func to clean it up.
// Loaders handle copying the relevant subset (SKILL.md, plugin.wasm,
// etc.) into the project's `.samuel/plugins/<name>/`.
package source

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samuelpkg/samuel/internal/errors"
)

// Component is the structured-error namespace.
const Component = "plugin/source"

// FetchRequest is what the install path passes to a Fetcher.
type FetchRequest struct {
	// Repo is one of the three accepted forms (file://, https://,
	// github.com/owner/repo).
	Repo string
	// Ref is the git ref (tag, branch, or commit). Empty means "default".
	Ref string
	// Subpath optionally narrows the materialized directory to a child
	// of the repository tree (used by upstream registry entries that
	// vendor multiple plugins in one repo).
	Subpath string
	// Workdir is an optional staging directory; empty means TempDir.
	Workdir string
	// Kind is the plugin kind hint from the registry index. When set to
	// "wasm" for a github.com/... repo, the default Fetcher prefers
	// release assets (plugin.wasm + cosign bundle) over a git clone.
	// Empty string preserves the legacy git-clone behavior for
	// skill / oci / meta tiers.
	Kind string
}

// Fetched describes the materialized source.
type Fetched struct {
	// Root is the absolute path to the plugin tree on disk.
	Root string
	// Cleanup removes any temporary files. Always callable; may be a no-op.
	Cleanup func()
}

// Fetcher abstracts how a plugin's bytes get to disk so tests can inject
// a local file:// source without git.
type Fetcher interface {
	Fetch(ctx context.Context, req FetchRequest) (*Fetched, error)
}

// Default returns the production Fetcher: file:// + git CLI.
func Default() Fetcher { return defaultFetcher{} }

type defaultFetcher struct{}

func (defaultFetcher) Fetch(ctx context.Context, req FetchRequest) (*Fetched, error) {
	repo := strings.TrimSpace(req.Repo)
	if repo == "" {
		return nil, &errors.Error{
			Component:   Component,
			Problem:     "fetch request has no repository",
			Recoverable: true,
		}
	}
	switch {
	case strings.HasPrefix(repo, "file://"):
		return fetchFile(req, strings.TrimPrefix(repo, "file://"))
	case strings.HasPrefix(repo, "github.com/") && strings.EqualFold(req.Kind, "wasm"):
		// PRD 0009 release-asset path: wasm-tier plugins ship
		// plugin.wasm + cosign bundle as github release assets, not in
		// the git tree. Pull from the release first; fall back to a
		// git clone when no release is present (legacy plugins, dev
		// snapshots) so non-released forks still work.
		owner, name, ok := splitGitHubRepo(repo)
		if ok {
			if got, rerr := fetchGitHubRelease(ctx, req, owner, name); rerr == nil {
				return got, nil
			}
		}
		return fetchGit(ctx, req, "https://"+repo+".git")
	case strings.HasPrefix(repo, "https://") || strings.HasPrefix(repo, "http://"):
		return fetchGit(ctx, req, repo)
	case strings.HasPrefix(repo, "github.com/"):
		return fetchGit(ctx, req, "https://"+repo+".git")
	default:
		return nil, &errors.Error{
			Component:   Component,
			Problem:     "unsupported plugin source",
			Cause:       repo,
			Fix:         "use a github.com/<owner>/<repo>, https://, or file:// URL",
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-PLUG-SOURCE-001",
			Recoverable: true,
		}
	}
}

func fetchFile(req FetchRequest, abs string) (*Fetched, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return nil, (&errors.Error{
			Component:   Component,
			Problem:     "file:// source not found",
			Path:        abs,
			Recoverable: true,
		}).Wrap(err)
	}
	root := abs
	if !info.IsDir() {
		// Treat a single file as the plugin payload (e.g. plugin.wasm).
		root = filepath.Dir(abs)
	}
	if req.Subpath != "" {
		root = filepath.Join(root, req.Subpath)
	}
	return &Fetched{Root: root, Cleanup: func() {}}, nil
}

func fetchGit(ctx context.Context, req FetchRequest, cloneURL string) (*Fetched, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, &errors.Error{
			Component:   Component,
			Problem:     "git not installed",
			Fix:         "install git, or use a file:// source for offline fixtures",
			Recoverable: true,
		}
	}
	parent := req.Workdir
	if parent == "" {
		var err error
		parent, err = os.MkdirTemp("", "samuel-fetch-*")
		if err != nil {
			return nil, err
		}
	}
	dest := filepath.Join(parent, "src")

	// Plugin repos in the wild tag releases as `vX.Y.Z` (Go / goreleaser
	// convention), but registry entries commonly publish the bare
	// `X.Y.Z` version. When the requested ref looks like a bare semver
	// and the first clone fails to find it, retry with a `v` prefix.
	// This is the semver-equivalence policy go modules adopted; keeping
	// it server-side here means registries don't have to mirror the
	// `v`-prefix bookkeeping.
	candidates := []string{req.Ref}
	if alt := vPrefixedSemver(req.Ref); alt != "" {
		candidates = append(candidates, alt)
	}

	var lastOut []byte
	var lastErr error
	for _, ref := range candidates {
		_ = os.RemoveAll(dest)
		args := []string{"clone", "--depth=1"}
		if ref != "" {
			args = append(args, "--branch", ref)
		}
		args = append(args, cloneURL, dest)
		cmd := exec.CommandContext(ctx, "git", args...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			// Drop the cloned .git/ before returning. Samuel resolves
			// installed plugins by name + lockfile digest, not by
			// walking commit history, so the git plumbing has no
			// downstream consumer. Leaving it in place inflates the
			// install (~120K of 176K on actix-web), surprises IDE git
			// integrations, and creates a nested git repo inside the
			// host project's tree.
			_ = os.RemoveAll(filepath.Join(dest, ".git"))
			root := dest
			if req.Subpath != "" {
				root = filepath.Join(dest, req.Subpath)
			}
			return &Fetched{
				Root:    root,
				Cleanup: func() { _ = os.RemoveAll(parent) },
			}, nil
		}
		lastOut, lastErr = out, err
		// Only fall through to the v-prefix retry on the "ref not
		// found" failure mode — every other failure (network, auth,
		// permissions) is reported as-is to avoid masking real issues.
		if !isRefNotFoundOutput(out) {
			break
		}
	}
	_ = os.RemoveAll(parent)
	return nil, (&errors.Error{
		Component:   Component,
		Problem:     "git clone failed",
		Cause:       fmt.Sprintf("%s: %s", lastErr, strings.TrimSpace(string(lastOut))),
		Recoverable: true,
	}).Wrap(lastErr)
}

// vPrefixedSemver returns "v"+ref when ref looks like a bare semver and
// does not already start with v. Returns "" otherwise. The check is
// intentionally lenient (any digit-led ref with at least one dot) so
// pre-release / build-metadata suffixes round-trip.
func vPrefixedSemver(ref string) string {
	if ref == "" || strings.HasPrefix(ref, "v") || strings.HasPrefix(ref, "V") {
		return ""
	}
	if ref[0] < '0' || ref[0] > '9' {
		return ""
	}
	if !strings.Contains(ref, ".") {
		return ""
	}
	return "v" + ref
}

// isRefNotFoundOutput reports whether the git clone failure was a
// missing-ref error rather than a network/auth/host failure. Used to
// gate the v-prefix retry — only retry when the ref itself is what
// failed, otherwise surface the real problem immediately.
func isRefNotFoundOutput(out []byte) bool {
	s := strings.ToLower(string(out))
	return strings.Contains(s, "remote branch") && strings.Contains(s, "not found")
}

// CopyTree copies src into dst recursively, preserving file mode bits.
// Used by skill-tier installs to land SKILL.md + assets/.
func CopyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, info.Mode().Perm()|0o100)
		}
		return copyFile(p, out, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
