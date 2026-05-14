package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// verifierFactories is the dual-run matrix used by the policy-surface
// tests below. Both implementations honor the same Request semantics
// for the short-circuit paths (signed_default=false, --allow-unsigned,
// registry in allow_unsigned_for). The cryptographic-math path is
// covered by sigstore_test.go with recorded Rekor fixtures.
//
// Adding a new entry here is the contract: every behavior tested in
// this file must hold across both verifiers without modification.
var verifierFactories = map[string]func() Verifier{
	"stub": func() Verifier { return StubVerifier{} },
	"sigstore": func() Verifier {
		// Hermetic SigstoreVerifier: no on-disk cache, no TUF fetch
		// (tests only exercise the short-circuit policy paths; the
		// cryptographic path is covered by sigstore_test.go).
		return NewSigstoreVerifier(DefaultPolicy(), WithTrustRootDir(""))
	},
}

// TestPolicy_RejectsUnknownSourceWhenSigningRequired asserts that under
// the default policy a source matching no identity_pattern fails. The
// stub fails with "stub verifier cannot verify it"; the sigstore
// verifier fails with a structured error citing the missing bundle —
// both surface a recoverable error.
func TestPolicy_RejectsUnknownSourceWhenSigningRequired(t *testing.T) {
	for name, mk := range verifierFactories {
		t.Run(name, func(t *testing.T) {
			v := mk()
			_, err := v.VerifyBlob(context.Background(), "/nonexistent", Request{
				Policy: DefaultPolicy(),
				Source: "github.com/random-stranger/plugin",
			})
			if err == nil {
				t.Fatalf("%s: should reject unknown source under default policy", name)
			}
		})
	}
}

// TestPolicy_AllowUnsignedBypasses asserts the CLI override short-circuits
// before any cryptographic work in either verifier.
func TestPolicy_AllowUnsignedBypasses(t *testing.T) {
	for name, mk := range verifierFactories {
		t.Run(name, func(t *testing.T) {
			v := mk()
			res, err := v.VerifyBlob(context.Background(), "/any", Request{
				Policy:        DefaultPolicy(),
				Source:        "github.com/random-stranger/plugin",
				AllowUnsigned: true,
			})
			if err != nil {
				t.Fatalf("%s: AllowUnsigned should bypass: %v", name, err)
			}
			if !res.Verified {
				t.Errorf("%s: expected verified=true", name)
			}
			if res.Reason != "--allow-unsigned" {
				t.Errorf("%s: expected Reason=--allow-unsigned, got %q", name, res.Reason)
			}
		})
	}
}

// TestPolicy_RegistryAllowlist asserts the per-registry allow_unsigned
// short-circuit is honored by both verifiers.
func TestPolicy_RegistryAllowlist(t *testing.T) {
	for name, mk := range verifierFactories {
		t.Run(name, func(t *testing.T) {
			pol := DefaultPolicy()
			pol.AllowUnsignedFor = []string{"local"}
			v := mk()
			// SigstoreVerifier owns its policy at construction; the
			// stub honors req.Policy. For parity, build a fresh
			// sigstore verifier with the same policy when needed.
			if name == "sigstore" {
				v = NewSigstoreVerifier(pol, WithTrustRootDir(""))
			}
			res, err := v.VerifyBlob(context.Background(), "/any", Request{
				Policy:   pol,
				Source:   "github.com/random/plugin",
				Registry: "local",
			})
			if err != nil || !res.Verified {
				t.Fatalf("%s: registry allowlist failed: ok=%v err=%v", name, res.Verified, err)
			}
		})
	}
}

// TestPolicy_SignedDefaultFalseBypasses asserts that setting
// signed_default=false in the policy bypasses verification in both
// implementations.
func TestPolicy_SignedDefaultFalseBypasses(t *testing.T) {
	for name, mk := range verifierFactories {
		t.Run(name, func(t *testing.T) {
			pol := DefaultPolicy()
			pol.SignedDefault = false
			v := mk()
			if name == "sigstore" {
				v = NewSigstoreVerifier(pol, WithTrustRootDir(""))
			}
			res, err := v.VerifyBlob(context.Background(), "/any", Request{
				Policy: pol,
				Source: "github.com/random/plugin",
			})
			if err != nil || !res.Verified {
				t.Fatalf("%s: signed_default=false should bypass: err=%v verified=%v", name, err, res.Verified)
			}
		})
	}
}

func TestStub_AcceptsSamuelpkgSource(t *testing.T) {
	v := StubVerifier{}
	res, err := v.VerifyBlob(context.Background(), "/any", Request{
		Policy: DefaultPolicy(),
		Source: "github.com/samuelpkg/samuel-go-guide",
	})
	if err != nil {
		t.Fatalf("samuelpkg/* should be accepted: %v", err)
	}
	if !res.Verified {
		t.Errorf("expected verified result")
	}
}

func TestMatchesIdentity(t *testing.T) {
	pol := DefaultPolicy()
	cases := map[string]bool{
		"github.com/samuelpkg/samuel-go-guide":         true,
		"https://github.com/samuelpkg/samuel-anything": true,
		"github.com/anthropics/skills/mcp-builder":     true,
		"github.com/random/plugin":                     false,
	}
	for src, want := range cases {
		if got := MatchesIdentity(pol, src); got != want {
			t.Errorf("MatchesIdentity(%q) = %v, want %v", src, got, want)
		}
	}
}

func TestCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	stubInner := stubVerifier{onBlob: func(string, Request) (Result, error) {
		calls++
		return Result{Verified: true, Identity: "test"}, nil
	}}
	cache := NewCache(dir, "v2.0.0", stubInner)
	path := filepath.Join(dir, "art.tar")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := Request{Policy: DefaultPolicy(), AllowUnsigned: true}
	if _, err := cache.VerifyBlob(context.Background(), path, req); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.VerifyBlob(context.Background(), path, req); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("inner verifier should be called once, got %d", calls)
	}
}

func TestCache_TogglingAllowUnsignedReVerifies(t *testing.T) {
	// Regression: prior to issue #2's fix, the cache keyed only on the
	// blob digest. First call with AllowUnsigned=true cached
	// Reason="--allow-unsigned"; second call with AllowUnsigned=false
	// returned the same cached entry, making the flag effectively
	// sticky and the policy invisible from the CLI.
	dir := t.TempDir()
	var calls []bool
	stubInner := stubVerifier{onBlob: func(_ string, req Request) (Result, error) {
		calls = append(calls, req.AllowUnsigned)
		if req.AllowUnsigned {
			return Result{Verified: true, Reason: "--allow-unsigned"}, nil
		}
		return Result{Verified: true, Reason: "stub: identity matched"}, nil
	}}
	cache := NewCache(dir, "v2.0.0", stubInner)
	path := filepath.Join(dir, "art.tar")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	r1, err := cache.VerifyBlob(context.Background(), path, Request{Policy: DefaultPolicy(), AllowUnsigned: true})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := cache.VerifyBlob(context.Background(), path, Request{Policy: DefaultPolicy(), AllowUnsigned: false})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Reason == r2.Reason {
		t.Errorf("AllowUnsigned toggle should change Reason; both were %q", r1.Reason)
	}
	if len(calls) != 2 {
		t.Errorf("inner verifier should be called twice (once per AllowUnsigned value), got %d", len(calls))
	}
	// Same call again with AllowUnsigned=true should hit the cache.
	if _, err := cache.VerifyBlob(context.Background(), path, Request{Policy: DefaultPolicy(), AllowUnsigned: true}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Errorf("second AllowUnsigned=true call should hit cache, got %d total calls", len(calls))
	}
}

func TestCache_InvalidatesOnVersionBump(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	stubInner := stubVerifier{onBlob: func(string, Request) (Result, error) {
		calls++
		return Result{Verified: true}, nil
	}}
	cacheV1 := NewCache(dir, "v2.0.0", stubInner)
	path := filepath.Join(dir, "art.tar")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := Request{Policy: DefaultPolicy(), AllowUnsigned: true}
	if _, err := cacheV1.VerifyBlob(context.Background(), path, req); err != nil {
		t.Fatal(err)
	}
	cacheV2 := NewCache(dir, "v2.0.1", stubInner)
	if _, err := cacheV2.VerifyBlob(context.Background(), path, req); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("version bump should invalidate cache, got %d calls", calls)
	}
}

// TestDefault_RespectsEnvOverride asserts that SAMUEL_VERIFY_STUB=1
// flips Default() back to the StubVerifier for tests and air-gapped
// runs. Without the env var, Default() returns the production
// sigstore-go backed verifier (IsProduction == true).
func TestDefault_RespectsEnvOverride(t *testing.T) {
	t.Setenv("SAMUEL_VERIFY_STUB", "1")
	if _, ok := Default().(StubVerifier); !ok {
		t.Errorf("Default() with SAMUEL_VERIFY_STUB=1 should return StubVerifier, got %T", Default())
	}
	if IsProduction() {
		t.Errorf("IsProduction() should be false when SAMUEL_VERIFY_STUB=1")
	}
}

// TestDefault_IsProductionByDefault asserts the v2.1 flip: with no env
// override, the default verifier is the sigstore-go production backend
// and IsProduction() reports true.
func TestDefault_IsProductionByDefault(t *testing.T) {
	// Explicitly clear the env var because the test process might
	// inherit it from a parent invocation.
	t.Setenv("SAMUEL_VERIFY_STUB", "")
	if _, ok := Default().(*SigstoreVerifier); !ok {
		t.Errorf("Default() should return *SigstoreVerifier in v2.1+, got %T", Default())
	}
	if !IsProduction() {
		t.Errorf("IsProduction() should be true with the production verifier as default")
	}
}

type stubVerifier struct {
	onBlob func(path string, req Request) (Result, error)
}

func (s stubVerifier) VerifyBlob(_ context.Context, path string, req Request) (Result, error) {
	return s.onBlob(path, req)
}
func (s stubVerifier) VerifyImage(_ context.Context, _ string, req Request) (Result, error) {
	return s.onBlob("", req)
}
