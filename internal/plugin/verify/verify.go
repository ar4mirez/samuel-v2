// Package verify is Samuel v2's signature-verification gate. The
// production implementation wraps sigstore-go to call cosign verify-blob
// (for skill / wasm archives) and cosign verify (for OCI image digests),
// but the public surface is a small interface so:
//
//   - tests can pass a stub Verifier with deterministic outcomes
//   - users with no sigstore tooling installed can still install plugins
//     gated by `--allow-unsigned`
//
// Policy comes from samuel.toml [security]:
//
//	[security]
//	signed_default = true
//	allow_unsigned_for = ["local", "dev"]
//	identity_patterns = [
//	  "https://github.com/samuelpkg/*",
//	  "https://github.com/anthropics/skills/*",
//	]
//	trusted_root = "https://tuf-repo-cdn.sigstore.dev"
//
// identity_patterns is OR-ed (any pattern match is enough) per RFD 0003
// resolution #3.
//
// Verification results are cached at ~/.samuel/cache/verify/ keyed by
// the artifact digest; the cache is invalidated whenever the samuel
// binary version changes (the cache filename embeds the binary version
// so a rebuild starts from an empty cache).
package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ar4mirez/samuel/internal/errors"
)

// Component is the structured-error namespace for this package.
const Component = "plugin/verify"

// Policy is the parsed [security] block.
type Policy struct {
	SignedDefault    bool     `toml:"signed_default"`
	AllowUnsignedFor []string `toml:"allow_unsigned_for,omitempty"`
	IdentityPatterns []string `toml:"identity_patterns,omitempty"`
	TrustedRoot      string   `toml:"trusted_root,omitempty"`
}

// DefaultPolicy is the starting policy used when samuel.toml has no
// [security] block: signed-by-default for the official registry, with
// the standard samuelpkg identity allowlist.
func DefaultPolicy() Policy {
	return Policy{
		SignedDefault:    true,
		AllowUnsignedFor: []string{"local", "dev"},
		IdentityPatterns: []string{
			"https://github.com/samuelpkg/*",
			"https://github.com/anthropics/skills/*",
		},
		TrustedRoot: "https://tuf-repo-cdn.sigstore.dev",
	}
}

// Verifier verifies a signed artifact against a policy. Production
// wires Sigstore; tests pass StubVerifier.
type Verifier interface {
	// VerifyBlob checks the signature/bundle for a file (skill archive,
	// wasm module). Returns the matched identity for audit logging or
	// "" when allow-unsigned forces a pass.
	VerifyBlob(ctx context.Context, artifactPath string, req Request) (Result, error)

	// VerifyImage checks the signature for an OCI image at digest.
	VerifyImage(ctx context.Context, imageDigest string, req Request) (Result, error)
}

// Request carries the policy + plugin context for one verification.
type Request struct {
	Policy        Policy
	Plugin        string
	Source        string // e.g. "github.com/samuelpkg/samuel-go-guide"
	Registry      string // registry name from samuel.toml
	BundlePath    string // optional sidecar bundle file (.bundle)
	AllowUnsigned bool   // CLI override
}

// Result is the verifier's verdict.
type Result struct {
	Verified bool
	Identity string
	Issuer   string
	Reason   string // free-form note (e.g. "allow-unsigned", "matched samuelpkg")
}

// StubVerifier always succeeds and reports identity "stub". Used by
// tests and as the default when sigstore-go is not wired in. It still
// honours the allow-unsigned policy: when the policy demands a
// signature and AllowUnsigned is false, it fails-closed unless the
// request explicitly opts into unsigned via the registry name.
type StubVerifier struct{}

func (StubVerifier) VerifyBlob(_ context.Context, _ string, req Request) (Result, error) {
	return decideStub(req)
}

func (StubVerifier) VerifyImage(_ context.Context, _ string, req Request) (Result, error) {
	return decideStub(req)
}

func decideStub(req Request) (Result, error) {
	if !req.Policy.SignedDefault {
		return Result{Verified: true, Reason: "policy.signed_default=false"}, nil
	}
	if req.AllowUnsigned {
		return Result{Verified: true, Reason: "--allow-unsigned"}, nil
	}
	if RegistryAllowsUnsigned(req.Policy, req.Registry) {
		return Result{Verified: true, Reason: "registry in allow_unsigned_for"}, nil
	}
	if MatchesIdentity(req.Policy, req.Source) {
		return Result{Verified: true, Identity: req.Source, Reason: "stub: identity matched"}, nil
	}
	return Result{}, &errors.Error{
		Component:   Component,
		Problem:     "signature required and stub verifier cannot verify it",
		Fix:         "install with --allow-unsigned, or add the source to [security].identity_patterns / allow_unsigned_for",
		DocsURL:     "https://ar4mirez.github.io/samuel/docs/errors/SAM-VERIFY-001",
		Recoverable: true,
	}
}

// MatchesIdentity reports whether source satisfies any identity_patterns
// glob. Patterns are OR-ed.
func MatchesIdentity(p Policy, source string) bool {
	if source == "" {
		return false
	}
	for _, pat := range p.IdentityPatterns {
		if globMatch(pat, source) {
			return true
		}
		if globMatch(pat, "https://"+source) {
			return true
		}
	}
	return false
}

// RegistryAllowsUnsigned reports whether the named registry is on the
// unsigned-allowlist.
func RegistryAllowsUnsigned(p Policy, name string) bool {
	for _, n := range p.AllowUnsignedFor {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// globMatch implements the limited "*" glob used in identity_patterns:
// "*" matches one path segment by default; "**" or trailing "*" match
// the rest of the string. Sufficient for "https://github.com/samuelpkg/*".
func globMatch(pattern, s string) bool {
	switch {
	case pattern == s:
		return true
	case pattern == "*":
		return true
	case strings.HasSuffix(pattern, "/*"):
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(s, prefix+"/")
	case strings.HasSuffix(pattern, "*"):
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(s, prefix)
	default:
		return false
	}
}

// Cache wraps a Verifier with a per-digest cache under cacheDir. The
// cache filename embeds samuelVersion so a rebuild starts fresh.
type Cache struct {
	dir            string
	samuelVersion  string
	wrapped        Verifier
}

// NewCache constructs a cache around v.
func NewCache(dir, samuelVersion string, v Verifier) *Cache {
	return &Cache{dir: dir, samuelVersion: samuelVersion, wrapped: v}
}

// VerifyBlob checks the cache, falls through to the inner verifier on
// miss, and writes the result back.
func (c *Cache) VerifyBlob(ctx context.Context, path string, req Request) (Result, error) {
	digest, err := blobDigest(path)
	if err != nil {
		return c.wrapped.VerifyBlob(ctx, path, req)
	}
	if r, ok := c.read(digest); ok {
		return r, nil
	}
	r, err := c.wrapped.VerifyBlob(ctx, path, req)
	if err == nil {
		_ = c.write(digest, r)
	}
	return r, err
}

// VerifyImage is the OCI counterpart; cache key is the digest verbatim.
func (c *Cache) VerifyImage(ctx context.Context, digest string, req Request) (Result, error) {
	if r, ok := c.read(digest); ok {
		return r, nil
	}
	r, err := c.wrapped.VerifyImage(ctx, digest, req)
	if err == nil {
		_ = c.write(digest, r)
	}
	return r, err
}

func (c *Cache) read(digest string) (Result, bool) {
	if c.dir == "" {
		return Result{}, false
	}
	body, err := os.ReadFile(c.path(digest))
	if err != nil {
		return Result{}, false
	}
	var out cachedResult
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, false
	}
	if out.SamuelVersion != c.samuelVersion {
		return Result{}, false
	}
	return out.Result, true
}

func (c *Cache) write(digest string, r Result) error {
	if c.dir == "" {
		return nil
	}
	path := c.path(digest)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(cachedResult{
		SamuelVersion: c.samuelVersion,
		WrittenAt:     time.Now().UTC().Format(time.RFC3339),
		Result:        r,
	})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Cache) path(digest string) string {
	short := digest
	if len(short) > 16 {
		short = short[:16]
	}
	return filepath.Join(c.dir, short+".json")
}

func blobDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

type cachedResult struct {
	SamuelVersion string `json:"samuel_version"`
	WrittenAt     string `json:"written_at"`
	Result        Result `json:"result"`
}

// Default returns the verifier the install path uses by default. v2.0
// ships StubVerifier; v2.1 will swap in a sigstore-go backed
// implementation gated by the same Verifier interface.
func Default() Verifier { return StubVerifier{} }

// Describe returns a one-line description of how a result will be
// rendered to the user.
func Describe(r Result) string {
	if !r.Verified {
		return "unverified"
	}
	if r.Identity != "" {
		return fmt.Sprintf("verified (%s)", r.Identity)
	}
	if r.Reason != "" {
		return "verified (" + r.Reason + ")"
	}
	return "verified"
}
