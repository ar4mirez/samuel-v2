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

	"github.com/samuelpkg/samuel/internal/errors"
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
		DocsURL:     "https://samuelpkg.github.io/samuel/docs/errors/SAM-VERIFY-001",
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
//
// The cache key includes the AllowUnsigned flag so that toggling
// --allow-unsigned on the CLI re-runs the policy check instead of
// returning a stale prior decision. Without this, the first `install
// --allow-unsigned` would make the flag effectively sticky for that
// file's digest on every subsequent install/update.
func (c *Cache) VerifyBlob(ctx context.Context, path string, req Request) (Result, error) {
	digest, err := blobDigest(path)
	if err != nil {
		return c.wrapped.VerifyBlob(ctx, path, req)
	}
	key := cacheKey(digest, req.AllowUnsigned)
	if r, ok := c.read(key); ok {
		return r, nil
	}
	r, err := c.wrapped.VerifyBlob(ctx, path, req)
	if err == nil {
		_ = c.write(key, r)
	}
	return r, err
}

// VerifyImage is the OCI counterpart; cache key is the digest plus the
// AllowUnsigned flag, for the same reason VerifyBlob keys on both.
func (c *Cache) VerifyImage(ctx context.Context, digest string, req Request) (Result, error) {
	key := cacheKey(digest, req.AllowUnsigned)
	if r, ok := c.read(key); ok {
		return r, nil
	}
	r, err := c.wrapped.VerifyImage(ctx, digest, req)
	if err == nil {
		_ = c.write(key, r)
	}
	return r, err
}

// cacheKey combines a content digest with the AllowUnsigned flag so the
// cache file path encodes both. Re-verifying with the flag flipped
// always misses the cache, which is what makes the policy responsive
// to CLI input on every invocation.
func cacheKey(digest string, allowUnsigned bool) string {
	if allowUnsigned {
		return digest + "+unsigned"
	}
	return digest
}

func (c *Cache) read(key string) (Result, bool) {
	if c.dir == "" {
		return Result{}, false
	}
	body, err := os.ReadFile(c.path(key))
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

func (c *Cache) write(key string, r Result) error {
	if c.dir == "" {
		return nil
	}
	path := c.path(key)
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

func (c *Cache) path(key string) string {
	// Truncate the digest portion for filesystem-friendly names but
	// keep the AllowUnsigned suffix intact so two policies for the
	// same blob land in different files.
	digest, suffix, found := strings.Cut(key, "+")
	if len(digest) > 16 {
		digest = digest[:16]
	}
	short := digest
	if found {
		short = digest + "+" + suffix
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

// IsProduction reports whether the default verifier performs
// cryptographic signature verification. v2.0 returns false because the
// default is StubVerifier — policy is enforced (identity_patterns,
// allow_unsigned_for, AllowUnsigned), but no Sigstore math runs. v2.1
// will flip this to true when the sigstore-go backend lands.
//
// Used by `samuel doctor` to surface a one-line disclosure so users
// know what "signature: verified (...)" currently means.
func IsProduction() bool {
	_, stub := Default().(StubVerifier)
	return !stub
}

// StubAdvisory is the one-line disclosure `samuel doctor` shows when
// IsProduction() returns false. Kept here so the wording lives next to
// the truth it describes.
const StubAdvisory = "verifier is stubbed in v2.0 — policy is enforced but signatures are not cryptographically validated. Real Sigstore verification ships in v2.1."

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
