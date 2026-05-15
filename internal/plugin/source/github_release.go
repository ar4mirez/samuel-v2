package source

// PRD 0009 release-asset fetcher: wasm-tier plugins publish
// `plugin.wasm` + the cosign bundle as github release assets rather
// than committing the binary to main. The default Fetcher routes
// `Kind == "wasm"` + `github.com/<owner>/<repo>` requests through
// this path so a `samuel install` resolves to the cosign-signed
// release artifact instead of a tree clone (which won't contain the
// built binary).
//
// Wire format under `https://github.com/<owner>/<repo>/releases/
// download/<tag>/<asset>`:
//
//   - plugin.wasm           (required)
//   - samuel-plugin.toml    (required — manifest)
//   - plugin.wasm.sig       (optional — cosign keyless)
//   - plugin.wasm.pem       (optional — cosign cert)
//
// All optional assets land on disk so the verifier sees them when
// `[security]` enforcement runs. Missing required assets bubble back
// to the caller, which falls through to fetchGit so legacy
// (binary-in-tree) plugins still install.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samuelpkg/samuel/internal/errors"
)

// githubReleaseAssets enumerates the filenames the wasm-tier release
// flow publishes (see internal/commands/new.go's scaffolded
// release.yml + samuelpkg/samuel-go-guide-wasm's .github/workflows/
// release.yml).
//
// `plugin.wasm.bundle` is the sigstore-go JSON bundle the verifier
// expects (internal/plugin/verify/sigstore.go's VerifyBlob looks for
// <artifact>.bundle alongside the artifact). The legacy `.sig`+`.pem`
// pair is still downloaded when present for inspection, but
// verification routes through the bundle.
var githubReleaseAssets = struct {
	required []string
	optional []string
}{
	required: []string{"plugin.wasm", "samuel-plugin.toml"},
	optional: []string{"plugin.wasm.bundle", "plugin.wasm.sig", "plugin.wasm.pem"},
}

// splitGitHubRepo parses `github.com/<owner>/<repo>` into its two
// path components. Returns ok=false for any other shape.
func splitGitHubRepo(repo string) (owner, name string, ok bool) {
	rest := strings.TrimPrefix(repo, "github.com/")
	if rest == repo {
		return "", "", false
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	// Strip any optional trailing path (subpath is honored separately
	// via FetchRequest.Subpath; the repo URL only needs owner/name).
	return parts[0], strings.TrimSuffix(parts[1], ".git"), true
}

// releaseTags returns the candidate tag names to try. github releases
// the wasm plugins under `vX.Y.Z` per goreleaser convention, but
// registry entries may publish either the bare or v-prefixed form;
// mirror the fetchGit fallback policy.
func releaseTags(ref string) []string {
	if ref == "" {
		return nil
	}
	out := []string{ref}
	if v := vPrefixedSemver(ref); v != "" {
		out = append(out, v)
	}
	return out
}

// fetchGitHubRelease downloads release assets for the resolved tag
// into <Workdir>/src/. Missing required files return an error so the
// caller can fall back to fetchGit; missing optional files are
// recorded silently.
func fetchGitHubRelease(ctx context.Context, req FetchRequest, owner, name string) (*Fetched, error) {
	if req.Ref == "" {
		return nil, &errors.Error{
			Component:   Component,
			Problem:     "wasm release fetch requires an explicit version",
			Fix:         "the registry index must publish `latest = \"X.Y.Z\"`",
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
	if err := os.MkdirAll(dest, 0o755); err != nil {
		_ = os.RemoveAll(parent)
		return nil, err
	}

	client := &http.Client{Timeout: 60 * time.Second}
	var lastErr error
	for _, tag := range releaseTags(req.Ref) {
		err := downloadReleaseAssets(ctx, client, owner, name, tag, dest)
		if err == nil {
			root := dest
			if req.Subpath != "" {
				root = filepath.Join(dest, req.Subpath)
			}
			return &Fetched{
				Root:    root,
				Cleanup: func() { _ = os.RemoveAll(parent) },
			}, nil
		}
		lastErr = err
		// Only fall through to the v-prefixed retry on a 404; other
		// errors (auth, network, partial write) should surface as-is.
		if !isReleaseNotFound(err) {
			break
		}
	}
	_ = os.RemoveAll(parent)
	return nil, lastErr
}

// downloadReleaseAssets pulls every required + optional asset for
// (owner, name, tag) into dest. Returns a release-not-found error
// when any required asset is missing.
func downloadReleaseAssets(ctx context.Context, client *http.Client, owner, name, tag, dest string) error {
	base := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s", owner, name, tag)
	for _, asset := range githubReleaseAssets.required {
		if err := downloadOne(ctx, client, base+"/"+asset, filepath.Join(dest, asset), true); err != nil {
			return err
		}
	}
	for _, asset := range githubReleaseAssets.optional {
		// Optional assets: a 404 is non-fatal; surface only network /
		// transport errors.
		if err := downloadOne(ctx, client, base+"/"+asset, filepath.Join(dest, asset), false); err != nil {
			if isReleaseNotFound(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// downloadOne fetches url into path. required=true treats a 404 as an
// error (used to gate the v-prefix retry); required=false returns a
// sentinel not-found error the caller can swallow.
func downloadOne(ctx context.Context, client *http.Client, url, path string, required bool) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return (&errors.Error{
			Component:   Component,
			Problem:     "release asset fetch failed",
			Cause:       url,
			Recoverable: true,
		}).Wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_ = required // both branches surface the same sentinel error; required only matters at the caller's switch
		return &releaseNotFoundError{url: url}
	}
	if resp.StatusCode >= 400 {
		return &errors.Error{
			Component:   Component,
			Problem:     fmt.Sprintf("release asset HTTP %d", resp.StatusCode),
			Cause:       url,
			Recoverable: true,
		}
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

type releaseNotFoundError struct{ url string }

func (e *releaseNotFoundError) Error() string {
	return "release asset not found: " + e.url
}

func isReleaseNotFound(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*releaseNotFoundError); ok {
		return true
	}
	// Unwrap one level — structured errors wrap the sentinel.
	if u, ok := err.(interface{ Unwrap() error }); ok {
		if _, inner := u.Unwrap().(*releaseNotFoundError); inner {
			return true
		}
	}
	return false
}
