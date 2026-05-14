# Tasks — PRD 0008: Real Sigstore Verifier (v2.1)

> Generated from [0008-prd-sigstore-verifier.md](0008-prd-sigstore-verifier.md) on 2026-05-13.
> Depends on PRD 0007 (Live-Registry E2E) being complete.
> Target release: v2.1.0.

## Relevant files

- `internal/plugin/verify/verify.go` — existing interface + `StubVerifier` + `Policy` + cache
- `internal/plugin/verify/verify_test.go` — existing test corpus (must run against both impls)
- `internal/plugin/verify/sigstore.go` — NEW production implementation
- `internal/plugin/verify/sigstore_test.go` — NEW unit tests with recorded Rekor fixtures
- `internal/plugin/verify/verify_bench_test.go` — NEW cold-verify + cache-hit benchmarks
- `internal/commands/doctor.go` — advisory line emission (drop stub language when IsProduction)
- `internal/commands/plugins.go` — install output (print signing identity from Result)
- `samuel-test-registry/` (from PRD 0007) — extend with signed fixtures
- `e2e/live/verify_live_test.go` — NEW
- `docs/concepts/signing.md` — drop stub paragraph; add sigstore-go sequence diagram
- `docs/reference/cli.md` — `--allow-unsigned` description tightening
- `docs/rfd/0009.md` — NEW RFD (Plugin signing via Sigstore enforcement)
- `rfd-index.toml` — register RFD 0009
- `CHANGELOG.md` — v2.1.0 entry
- `go.mod` / `go.sum` — `github.com/sigstore/sigstore-go` pin

## Tasks

- [x] 1.0 sigstore-go dependency + skeleton [~2,500 tokens - Simple]
  - [x] 1.1 Add `github.com/sigstore/sigstore-go` to `go.mod` at an exact version pin
  - [x] 1.2 Run license audit on transitive deps; fail on GPL/AGPL
  - [x] 1.3 Scaffold `internal/plugin/verify/sigstore.go` with `SigstoreVerifier` struct + interface stub returning `errors.New("not implemented")` for each method
  - [x] 1.4 Wire constructor `NewSigstoreVerifier(policy Policy, opts ...Option)`
  - [x] 1.5 Confirm `go build ./...` clean with the skeleton

- [x] 2.0 verifyBlob implementation [~4,000 tokens - Medium]
  - [x] 2.1 Implement `verifyBlob(ctx, digest, signatureBundle, identityPatterns)` against sigstore-go's blob-signature primitives
  - [x] 2.2 Identity-pattern matching reuses existing `verify.matchPattern` helper
  - [x] 2.3 Map sigstore-go error types → samuel structured errors with DocsURL
  - [x] 2.4 Unit test against a hand-crafted Rekor response fixture (no network)

- [x] 3.0 verifyOCI implementation [~3,500 tokens - Medium]
  - [x] 3.1 Implement `verifyOCI(ctx, imageDigest, identityPatterns)` against sigstore-go's container-signature primitives
  - [x] 3.2 Unit test against a public cosign-signed image (cosign's own release image)
  - [x] 3.3 Document the image-ref normalization (registry/repo@digest) in inline comments

- [x] 4.0 TUF trust root + cache [~3,000 tokens - Medium]
  - [x] 4.1 Lazy-fetch TUF root from `https://tuf-repo-cdn.sigstore.dev` on first verify call
  - [x] 4.2 Cache at `~/.samuel/cache/sigstore/trust-root/` keyed by binary version (mirrors existing verify cache)
  - [x] 4.3 24h TTL; refresh on miss; structured error on persistent fetch failure
  - [x] 4.4 Retry 3x with backoff on transient network failure
  - [x] 4.5 Document `SAMUEL_TUF_MIRROR` env var as future hook (not implemented this PRD)

- [x] 5.0 Default factory + IsProduction [~2,000 tokens - Simple]
  - [x] 5.1 Update `verify.Default()` to return `*SigstoreVerifier` by default
  - [x] 5.2 Honor `SAMUEL_VERIFY_STUB=1` env var → returns `*StubVerifier` (test escape hatch)
  - [x] 5.3 `IsProduction()` returns `true` iff `Default()` returns `*SigstoreVerifier`
  - [x] 5.4 Update existing tests that relied on `StubVerifier` as default; flip via env var or explicit construction

- [x] 6.0 Doctor advisory + install output [~2,500 tokens - Simple]
  - [x] 6.1 In `internal/commands/doctor.go`: suppress "policy-only stub" advisory when `verify.IsProduction()`
  - [x] 6.2 Replace with `signature verifier: sigstore-go (production)` line
  - [x] 6.3 When stub-mode active (env override), print `signature verifier: stub (test mode)` on every install (not just doctor)
  - [x] 6.4 `samuel install` success line prints actual identity from `Result`: `installed foo@1.0.0 (signed by https://github.com/...)` 
  - [x] 6.5 Failure includes Rekor log entry URL: `rekor: https://rekor.sigstore.dev/api/v1/log/entries/...`

- [x] 7.0 Test corpus dual-run [~2,500 tokens - Simple]
  - [x] 7.1 Wrap existing `verify_test.go` tests in `t.Run("stub", ...)` / `t.Run("sigstore", ...)` subtests
  - [x] 7.2 Confirm both paths pass identical assertions where policy is identical
  - [x] 7.3 Write `sigstore_test.go` with recorded Rekor fixtures committed to `testdata/sigstore/`
  - [x] 7.4 Document fixture-rotation playbook (when sigstore-go upstream rotates trust roots)

- [x] 8.0 Signed fixtures in samuel-test-registry [~3,500 tokens - Medium]
  - [x] 8.1 Add `samuel-test-skill-signed` (signed against `https://github.com/samuelpkg/samuel-test-registry/*` identity)
  - [x] 8.2 Add `samuel-test-skill-unsigned` (explicitly unsigned; test asserts install fails without `--allow-unsigned`)
  - [x] 8.3 Add `samuel-test-skill-wrong-identity` (signed against a non-matching identity; test asserts verify rejects)
  - [x] 8.4 Author test registry release workflow: cosign sign-blob with OIDC on tag push
  - [x] 8.5 Verify each fixture's signature manually with `cosign verify-blob` from a clean clone

- [x] 9.0 Live e2e signing tests [~2,500 tokens - Simple]
  - [x] 9.1 `e2e/live/verify_live_test.go`: `TestVerify_SignedFixture_Verifies` — install signed fixture; assert `Verified: true`
  - [x] 9.2 `TestVerify_UnsignedFixture_RejectsWithoutFlag` — expect structured error with DocsURL
  - [x] 9.3 `TestVerify_UnsignedFixture_AcceptsWithFlag` — `--allow-unsigned`; assert `Reason: --allow-unsigned`
  - [x] 9.4 `TestVerify_WrongIdentity_Rejects` — expect failure citing identity-pattern mismatch

- [x] 10.0 Benchmarks + perf budget [~2,000 tokens - Simple]
  - [x] 10.1 `verify_bench_test.go`: `BenchmarkColdVerify_NoCache` — cold path with TUF fetch
  - [x] 10.2 `BenchmarkColdVerify_CachedTrust` — TUF cached, fresh verify
  - [x] 10.3 `BenchmarkWarmVerify_FullCacheHit` — full result cache hit
  - [x] 10.4 Assert cold ≤ 3s, cache-hit ≤ 50ms on reference laptop
  - [x] 10.5 Document budget in `docs/concepts/signing.md`

- [x] 11.0 Registry index `signature_bundle` field [~1,500 tokens - Simple]
  - [x] 11.1 Document the existing nullable `signature_bundle` URL field in `index.toml`
  - [x] 11.2 For v2.1 registry indexes, plugins SHOULD include the bundle URL
  - [x] 11.3 Verify code: if bundle missing AND `--allow-unsigned` not set, structured error pointing to identity-pattern docs
  - [x] 11.4 Update production `samuel-registry/README.md` with the v2.1 expectation

- [x] 12.0 Documentation + RFD 0009 [~4,000 tokens - Medium]
  - [x] 12.1 Rewrite `docs/concepts/signing.md`: drop stub-disclosure paragraph
  - [x] 12.2 Add "How sigstore-go verification works" with sequence diagram (TUF fetch → Rekor lookup → signature math → identity match → result cache)
  - [x] 12.3 Update `docs/reference/cli.md`: `samuel install --help` no stub language
  - [x] 12.4 Draft `docs/rfd/0009.md` — "Plugin signing via Sigstore enforcement (v2.1)"
  - [x] 12.5 Update `rfd-index.toml` with RFD 0009
  - [x] 12.6 Cross-link from `wiki/synthesis/v2-rc-cycle-lessons.md` "Honest disclosure" section to the new RFD

- [x] 13.0 Release v2.1.0 [~2,000 tokens - Simple]
  - [x] 13.1 CHANGELOG `## [v2.1.0]` entry: lede = math swap; `IsProduction()` flips
  - [x] 13.2 Note the wire-format + lockfile stability across the transition (no migration)
  - [x] 13.3 Tag `v2.1.0-rc.1`; verify v2.1.0-rc.1 binary self-verifies its own checksum bundle
  - [x] 13.4 After 1 week soak: tag `v2.1.0`
  - [x] 13.5 Announce: signing claims now backed by math (link to RFD 0009)
