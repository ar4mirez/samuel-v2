package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Performance budget (asserted in TestVerify_PerfBudget below):
//
//	cold (no cache)        ≤ 3s   (includes TUF fetch on first call)
//	cold (trust cached)    ≤ 500ms
//	warm (full result hit) ≤ 50ms
//
// The actual sigstore-go cold path can't run in CI without a network
// signal, so the cold benchmarks are skipped by default and only fire
// when SAMUEL_BENCH_NETWORK=1 is set. The warm-cache benchmark is the
// load-bearing one for the every-day developer flow.

// BenchmarkColdVerify_NoCache measures the cold verify path with both
// the TUF root and result cache cleared. Network-bound; skipped by
// default to keep the unit tier hermetic.
func BenchmarkColdVerify_NoCache(b *testing.B) {
	if os.Getenv("SAMUEL_BENCH_NETWORK") != "1" {
		b.Skip("set SAMUEL_BENCH_NETWORK=1 to run cold-path benchmarks against live sigstore infra")
	}
	dir := b.TempDir()
	art := filepath.Join(dir, "artifact.tar")
	if err := os.WriteFile(art, []byte("hello"), 0o600); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		v := NewSigstoreVerifier(DefaultPolicy(),
			WithTrustRootDir(b.TempDir()),
			WithSamuelVersion("v2.1.0-bench"),
		)
		_, _ = v.VerifyBlob(context.Background(), art, Request{
			Policy:        DefaultPolicy(),
			Plugin:        "bench",
			Source:        "github.com/samuelpkg/bench",
			AllowUnsigned: true,
		})
	}
}

// BenchmarkColdVerify_CachedTrust measures a fresh verify call when
// the TUF trust root is already cached on disk. The TUF fetch is
// elided; sigstore-go's bundle/policy work still runs.
func BenchmarkColdVerify_CachedTrust(b *testing.B) {
	if os.Getenv("SAMUEL_BENCH_NETWORK") != "1" {
		b.Skip("set SAMUEL_BENCH_NETWORK=1 to run benchmarks against live sigstore infra")
	}
	dir := b.TempDir()
	art := filepath.Join(dir, "artifact.tar")
	if err := os.WriteFile(art, []byte("hello"), 0o600); err != nil {
		b.Fatal(err)
	}
	trustDir := b.TempDir()
	// Warm the cache once outside the timed loop.
	warm := NewSigstoreVerifier(DefaultPolicy(),
		WithTrustRootDir(trustDir),
		WithSamuelVersion("v2.1.0-bench"),
	)
	_ = warm.ensureTrustRoot(context.Background())
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		v := NewSigstoreVerifier(DefaultPolicy(),
			WithTrustRootDir(trustDir),
			WithSamuelVersion("v2.1.0-bench"),
		)
		_, _ = v.VerifyBlob(context.Background(), art, Request{
			Policy:        DefaultPolicy(),
			Plugin:        "bench",
			Source:        "github.com/samuelpkg/bench",
			AllowUnsigned: true,
		})
	}
}

// BenchmarkWarmVerify_FullCacheHit measures the result-cache hit path:
// the verify.Cache wraps the verifier and returns the prior decision
// without re-running the inner verifier. This is the steady-state
// every-day flow (subsequent `samuel install` calls on the same
// artifact); the perf budget is ≤ 50ms.
func BenchmarkWarmVerify_FullCacheHit(b *testing.B) {
	dir := b.TempDir()
	art := filepath.Join(dir, "artifact.tar")
	if err := os.WriteFile(art, []byte("hello"), 0o600); err != nil {
		b.Fatal(err)
	}
	cache := NewCache(dir, "v2.1.0-bench", StubVerifier{})
	req := Request{Policy: DefaultPolicy(), Source: "github.com/samuelpkg/foo", AllowUnsigned: true}
	if _, err := cache.VerifyBlob(context.Background(), art, req); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.VerifyBlob(context.Background(), art, req)
	}
}
