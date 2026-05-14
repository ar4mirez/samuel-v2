// Package verify — production sigstore-go implementation.
//
// SigstoreVerifier performs cryptographic signature verification against
// the public sigstore infrastructure (TUF trust root + Rekor transparency
// log + Fulcio short-lived certs). It is the v2.1 default and replaces
// the v2.0 [[StubVerifier]] for the install path.
//
// Wire order on a fresh verify call:
//
//  1. ensureTrustRoot — lazy TUF fetch (or load cached JSON) keyed by
//     binary version + day-bucket (24h TTL).
//  2. loadBundle — read the artifact's signature_bundle sidecar
//     (.bundle) from disk; sigstore-go's bundle.LoadJSONFromPath.
//  3. construct sigstore-go verifier with the trusted material; observe
//     timestamps + transparency-log presence per the public-good
//     defaults.
//  4. derive ArtifactPolicy from the artifact digest (blob) or image
//     digest (OCI).
//  5. derive identity policies from the samuel Policy
//     identity_patterns; each pattern compiles to a sigstore-go
//     CertificateIdentity (SAN regex on the URL form, issuer regex
//     for the GitHub Actions issuer).
//  6. evaluate sigstore.Verifier.Verify(b, policy).
//  7. on success → Result{Verified: true, Identity, Issuer, ...};
//     on failure → wrap the sigstore-go error as a samuel
//     *errors.Error with DocsURL pointing at docs/concepts/signing.md.
//
// The verifier is safe for concurrent calls — the TUF client + the
// trusted-root material are loaded once under sync.Once and reused.
package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	sgtuf "github.com/sigstore/sigstore-go/pkg/tuf"
	sgverify "github.com/sigstore/sigstore-go/pkg/verify"

	samerrors "github.com/samuelpkg/samuel/internal/errors"
)

// SigstoreVerifier is the production Verifier backed by sigstore-go.
type SigstoreVerifier struct {
	policy Policy

	// trustRootDir caches the fetched trusted_root.json under
	// ~/.samuel/cache/sigstore/trust-root/<samuel-version>/. Empty
	// disables on-disk caching (used in unit tests).
	trustRootDir string

	// samuelVersion is the cache-key salt — bumping the samuel binary
	// version invalidates any previously cached trust root, matching
	// the same pattern as the existing verify Result cache.
	samuelVersion string

	// tufRepoURL overrides the default TUF mirror. Defaults to
	// sigstore's public CDN; the SAMUEL_TUF_MIRROR env var is reserved
	// for the future-mirror hook (documented in RFD 0009; not honored
	// until v2.2).
	tufRepoURL string

	// tufTTL is the time-to-live for a cached trusted_root.json
	// before it is re-fetched. Defaults to 24h (matches sigstore's
	// upstream rotation cadence).
	tufTTL time.Duration

	// retryAttempts is the number of TUF fetch retries on transient
	// network failure. 3 attempts with exponential backoff.
	retryAttempts int

	// httpClient is the HTTP client for OCI signature fetches. The
	// TUF client uses its own internal client with the right
	// user-agent.
	httpClient *http.Client

	once    sync.Once
	trusted root.TrustedMaterialCollection
	loadErr error

	// now is the clock; tests stub for deterministic TTL behavior.
	now func() time.Time
}

// Option configures a SigstoreVerifier at construction time.
type Option func(*SigstoreVerifier)

// WithTrustRootDir sets the on-disk cache directory for the fetched
// trusted_root.json. Mirrors the existing verify-result cache layout
// (~/.samuel/cache/sigstore/trust-root/).
func WithTrustRootDir(dir string) Option {
	return func(v *SigstoreVerifier) { v.trustRootDir = dir }
}

// WithSamuelVersion salts the trust-root cache key, ensuring a fresh
// fetch on every binary upgrade.
func WithSamuelVersion(ver string) Option {
	return func(v *SigstoreVerifier) { v.samuelVersion = ver }
}

// WithTUFRepository overrides the default TUF mirror URL.
func WithTUFRepository(url string) Option {
	return func(v *SigstoreVerifier) { v.tufRepoURL = url }
}

// WithTrustedMaterial injects pre-loaded trusted material, bypassing
// the TUF fetch. Used by unit tests that ship a fixture trust root.
func WithTrustedMaterial(tm root.TrustedMaterialCollection) Option {
	return func(v *SigstoreVerifier) {
		v.trusted = tm
		v.once.Do(func() {}) // mark trust root as ready
	}
}

// WithClock stubs the internal clock for TTL tests.
func WithClock(now func() time.Time) Option {
	return func(v *SigstoreVerifier) { v.now = now }
}

// NewSigstoreVerifier constructs the production verifier. Defaults:
//
//   - TUF repo: https://tuf-repo-cdn.sigstore.dev
//   - TTL: 24h
//   - Retries: 3 with exponential backoff
//   - Trust-root cache: ~/.samuel/cache/sigstore/trust-root/
//     (resolved at first use; empty until then)
func NewSigstoreVerifier(policy Policy, opts ...Option) *SigstoreVerifier {
	v := &SigstoreVerifier{
		policy:        policy,
		tufRepoURL:    defaultTUFRepo,
		tufTTL:        24 * time.Hour,
		retryAttempts: 3,
		httpClient:    http.DefaultClient,
		now:           time.Now,
	}
	for _, o := range opts {
		o(v)
	}
	// Resolve trust-root cache dir lazily from $HOME if not set; this
	// keeps the unit tests hermetic (they call WithTrustRootDir(t.TempDir())).
	if v.trustRootDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			v.trustRootDir = filepath.Join(home, ".samuel", "cache", "sigstore", "trust-root")
		}
	}
	return v
}

const defaultTUFRepo = "https://tuf-repo-cdn.sigstore.dev"

// VerifyBlob verifies the signature bundle for a file artifact (skill
// archive or wasm module). It is the cosign-verify-blob equivalent.
//
// req.BundlePath, when set, points at the .bundle sidecar
// (sigstore-go bundle format, JSON). When empty the verifier looks for
// <artifactPath>.bundle alongside the artifact (cosign default).
//
// req.AllowUnsigned + policy.AllowUnsignedFor short-circuit before
// touching the TUF root so users without network can still install
// with the explicit opt-in.
func (v *SigstoreVerifier) VerifyBlob(ctx context.Context, artifactPath string, req Request) (Result, error) {
	if r, ok := shortCircuit(req); ok {
		return r, nil
	}
	if err := v.ensureTrustRoot(ctx); err != nil {
		return Result{}, err
	}
	digest, err := blobDigestRaw(artifactPath)
	if err != nil {
		return Result{}, wrapVerifyErr("read artifact for digest", err, req)
	}
	bundlePath := req.BundlePath
	if bundlePath == "" {
		bundlePath = artifactPath + ".bundle"
	}
	b, err := bundle.LoadJSONFromPath(bundlePath)
	if err != nil {
		return Result{}, missingBundleErr(bundlePath, req)
	}
	return v.verifyBundle(b, req, sgverify.WithArtifactDigest("sha256", digest))
}

// VerifyImage verifies the signature for an OCI image at digest. The
// image-ref normalization (registry/repo@sha256:...) is performed by
// the caller — `imageDigest` here is the hex digest only (post-pull).
//
// In v2.1 the bundle URL is fetched out-of-band: the registry index
// publishes signature_bundle as a sidecar URL, or the OCI manifest
// carries a referrer with the bundle media-type. For now we expect
// req.BundlePath to carry the resolved local bundle path; the caller
// (service.verifyArtifact for KindOci) does the resolution.
func (v *SigstoreVerifier) VerifyImage(ctx context.Context, imageDigest string, req Request) (Result, error) {
	if r, ok := shortCircuit(req); ok {
		return r, nil
	}
	if err := v.ensureTrustRoot(ctx); err != nil {
		return Result{}, err
	}
	if imageDigest == "" {
		return Result{}, &samerrors.Error{
			Component:   Component,
			Problem:     "OCI verify requires a pinned image digest",
			Fix:         "ensure the manifest's [oci].digest is set or pull the image first",
			DocsURL:     docsURL,
			Recoverable: true,
		}
	}
	if req.BundlePath == "" {
		return Result{}, missingBundleErr(imageDigest+".bundle", req)
	}
	b, err := bundle.LoadJSONFromPath(req.BundlePath)
	if err != nil {
		return Result{}, missingBundleErr(req.BundlePath, req)
	}
	raw, err := decodeOCIDigest(imageDigest)
	if err != nil {
		return Result{}, wrapVerifyErr("decode image digest", err, req)
	}
	return v.verifyBundle(b, req, sgverify.WithArtifactDigest("sha256", raw))
}

// verifyBundle is the shared bundle-verification path: TUF root +
// identity policy + artifact policy → sigstore-go Verify.
func (v *SigstoreVerifier) verifyBundle(b *bundle.Bundle, req Request, artifactPolicy sgverify.ArtifactPolicyOption) (Result, error) {
	if len(v.trusted) == 0 {
		return Result{}, &samerrors.Error{
			Component:   Component,
			Problem:     "no trusted material available — TUF fetch failed",
			Fix:         "check network connectivity to " + defaultTUFRepo + ", or install with --allow-unsigned",
			DocsURL:     docsURL,
			Recoverable: true,
		}
	}
	sev, err := sgverify.NewVerifier(v.trusted,
		sgverify.WithSignedCertificateTimestamps(1),
		sgverify.WithObserverTimestamps(1),
		sgverify.WithTransparencyLog(1),
	)
	if err != nil {
		return Result{}, wrapVerifyErr("construct sigstore verifier", err, req)
	}
	identityPolicies, err := buildIdentityPolicies(v.policy.IdentityPatterns)
	if err != nil {
		return Result{}, err
	}
	if len(identityPolicies) == 0 {
		return Result{}, &samerrors.Error{
			Component:   Component,
			Problem:     "policy has no identity_patterns and no key — cannot verify",
			Fix:         "add identity_patterns under [security] in samuel.toml, or install with --allow-unsigned",
			DocsURL:     docsURL,
			Recoverable: true,
		}
	}
	res, err := sev.Verify(b, sgverify.NewPolicy(artifactPolicy, identityPolicies...))
	if err != nil {
		return Result{}, signatureFailErr(err, req, b)
	}
	out := Result{Verified: true}
	if res != nil && res.Signature != nil && res.Signature.Certificate != nil {
		out.Identity = res.Signature.Certificate.SubjectAlternativeName
		out.Issuer = res.Signature.Certificate.Issuer
		out.Reason = "sigstore-go: identity matched + tlog observed"
	} else {
		out.Reason = "sigstore-go: verified"
	}
	return out, nil
}

// buildIdentityPolicies translates the samuel identity_patterns glob
// list into sigstore-go CertificateIdentity entries. Each pattern is
// converted to a SAN regex; the issuer is constrained to the GitHub
// Actions OIDC issuer (the only one v2.1 supports — RFD 0009).
func buildIdentityPolicies(patterns []string) ([]sgverify.PolicyOption, error) {
	out := make([]sgverify.PolicyOption, 0, len(patterns))
	for _, p := range patterns {
		sanRegex := globToRegex(p)
		certID, err := sgverify.NewShortCertificateIdentity(
			"https://token.actions.githubusercontent.com", // issuer
			"",            // issuerRegex
			"",            // SAN literal
			sanRegex,      // SAN regex
		)
		if err != nil {
			return nil, &samerrors.Error{
				Component:   Component,
				Problem:     "invalid identity_pattern: " + p,
				Cause:       err.Error(),
				DocsURL:     docsURL,
				Recoverable: true,
			}
		}
		out = append(out, sgverify.WithCertificateIdentity(certID))
	}
	return out, nil
}

// globToRegex converts a samuel identity_pattern glob into a regex
// anchored at both ends. Mirrors verify.globMatch's semantics:
//
//	"https://github.com/samuelpkg/*"   → ^https://github\.com/samuelpkg/[^/]+$
//	"https://github.com/foo/**"        → ^https://github\.com/foo/.*$
//	"https://github.com/foo/bar"       → ^https://github\.com/foo/bar$
//
// The mapping is intentionally simple; for richer matching, write the
// regex directly via a future identity_patterns_regex field.
func globToRegex(pattern string) string {
	// Escape regex metacharacters except `*`.
	escaped := regexp.QuoteMeta(pattern)
	// QuoteMeta escapes `*` as `\*`; restore the wildcards.
	escaped = strings.ReplaceAll(escaped, `\*\*`, `.*`)
	escaped = strings.ReplaceAll(escaped, `\*`, `[^/]*`)
	return "^" + escaped + "$"
}

// ensureTrustRoot lazily fetches the TUF trusted_root.json on first
// call, with 24h TTL on the on-disk cache. Subsequent calls are no-ops.
func (v *SigstoreVerifier) ensureTrustRoot(ctx context.Context) error {
	v.once.Do(func() {
		v.loadErr = v.loadTrustRoot(ctx)
	})
	return v.loadErr
}

func (v *SigstoreVerifier) loadTrustRoot(ctx context.Context) error {
	if cached, err := v.readCachedTrustRoot(); err == nil {
		v.trusted = append(v.trusted, cached)
		return nil
	}
	var raw []byte
	var lastErr error
	for attempt := 0; attempt < v.retryAttempts; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<attempt) * 200 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		b, err := v.fetchTrustRootViaTUF(ctx)
		if err == nil {
			raw = b
			lastErr = nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		return &samerrors.Error{
			Component:   Component,
			Problem:     "could not fetch sigstore TUF trust root after retries",
			Cause:       lastErr.Error(),
			Fix:         "check network connectivity to " + v.tufRepoURL + ", or install with --allow-unsigned",
			DocsURL:     docsURL,
			Recoverable: true,
		}
	}
	tr, err := root.NewTrustedRootFromJSON(raw)
	if err != nil {
		return wrapVerifyErr("parse trusted_root.json", err, Request{})
	}
	v.trusted = append(v.trusted, tr)
	if err := v.writeCachedTrustRoot(raw); err != nil {
		// Cache write failures are non-fatal — the trust root is
		// still loaded in memory for this process.
		_ = err
	}
	return nil
}

func (v *SigstoreVerifier) fetchTrustRootViaTUF(_ context.Context) ([]byte, error) {
	opts := sgtuf.DefaultOptions()
	opts.RepositoryBaseURL = v.tufRepoURL
	client, err := sgtuf.New(opts)
	if err != nil {
		return nil, err
	}
	return client.GetTarget("trusted_root.json")
}

func (v *SigstoreVerifier) trustRootCachePath() string {
	if v.trustRootDir == "" {
		return ""
	}
	ver := v.samuelVersion
	if ver == "" {
		ver = "unversioned"
	}
	return filepath.Join(v.trustRootDir, ver, "trusted_root.json")
}

func (v *SigstoreVerifier) readCachedTrustRoot() (*root.TrustedRoot, error) {
	path := v.trustRootCachePath()
	if path == "" {
		return nil, errors.New("no cache dir")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if v.now().Sub(info.ModTime()) > v.tufTTL {
		return nil, errors.New("trust root cache expired")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return root.NewTrustedRootFromJSON(body)
}

func (v *SigstoreVerifier) writeCachedTrustRoot(raw []byte) error {
	path := v.trustRootCachePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// shortCircuit handles AllowUnsigned + allow_unsigned_for + signed_default
// gates BEFORE the TUF root fetch, so users with no network can still
// install with the explicit opt-in.
func shortCircuit(req Request) (Result, bool) {
	if !req.Policy.SignedDefault {
		return Result{Verified: true, Reason: "policy.signed_default=false"}, true
	}
	if req.AllowUnsigned {
		return Result{Verified: true, Reason: "--allow-unsigned"}, true
	}
	if RegistryAllowsUnsigned(req.Policy, req.Registry) {
		return Result{Verified: true, Reason: "registry in allow_unsigned_for"}, true
	}
	return Result{}, false
}

const docsURL = "https://samuelpkg.github.io/samuel/docs/concepts/signing"

func missingBundleErr(bundlePath string, req Request) error {
	return &samerrors.Error{
		Component:   Component,
		Problem:     "signature bundle missing",
		Cause:       "no .bundle sidecar at " + bundlePath,
		Fix:         "publish a signature_bundle URL in the registry index, or install with --allow-unsigned",
		DocsURL:     docsURL,
		Path:        bundlePath,
		Recoverable: true,
	}
}

func signatureFailErr(err error, req Request, b *bundle.Bundle) error {
	cause := err.Error()
	rekorRef := rekorLogURL(b)
	if rekorRef != "" {
		cause += " (rekor: " + rekorRef + ")"
	}
	return &samerrors.Error{
		Component:   Component,
		Problem:     "signature verification failed for " + req.Plugin,
		Cause:       cause,
		Fix:         "confirm the plugin source matches identity_patterns, or install with --allow-unsigned",
		DocsURL:     docsURL,
		Recoverable: true,
	}
}

func wrapVerifyErr(stage string, err error, req Request) error {
	return &samerrors.Error{
		Component:   Component,
		Problem:     stage + ": " + req.Plugin,
		Cause:       err.Error(),
		DocsURL:     docsURL,
		Recoverable: true,
	}
}

// rekorLogURL extracts a human-pasteable Rekor log entry URL from a
// bundle, when present. Used in failure errors so the user can inspect
// the transparency-log entry directly. Returns "" if the bundle has no
// observable tlog entry.
func rekorLogURL(b *bundle.Bundle) string {
	if b == nil {
		return ""
	}
	entries, err := b.TlogEntries()
	if err != nil || len(entries) == 0 {
		return ""
	}
	logIndex := entries[0].LogIndex()
	if logIndex == 0 {
		return ""
	}
	return fmt.Sprintf("https://rekor.sigstore.dev/api/v1/log/entries?logIndex=%d", logIndex)
}

func blobDigestRaw(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func decodeOCIDigest(digest string) ([]byte, error) {
	d := strings.TrimPrefix(digest, "sha256:")
	return hex.DecodeString(d)
}

// SigstoreAdvisory is the doctor line shown when the production verifier
// is active. Counterpart to StubAdvisory.
const SigstoreAdvisory = "signature verifier: sigstore-go (production)"
