---
prd: "0008"
milestone: "Real Sigstore verification (v2.1)"
title: Samuel v2.1 — swap StubVerifier for sigstore-go
authors:
  - name: ar4mirez
state: Draft
labels: [v2, v2.1, signing, sigstore, security, plugin-verify]
created: 2026-05-13
updated: 2026-05-13
target_release: v2.1.0
estimated_effort: 2 weeks
depends_on: 0007-prd-live-registry-e2e.md
---

# PRD 0008: Real Sigstore Verifier (v2.1)

## Wiki references

- [[synthesis/v2-rc-cycle-lessons]] — "Honest disclosure of the verifier stub (rc.10)" section
- [[concepts/versioning-compatibility]] — capability + signing model
- [[CLAUDE]] — "Sigstore/cosign signed-by-default for the official registry. v2.0 ships a policy-only StubVerifier; sigstore-go math swap is v2.1."
- [GitHub Issue #6 (final close)](https://github.com/samuelpkg/samuel/issues/6) — tracking issue

## Summary

Replace `verify.StubVerifier` with a real `sigstore-go`-backed implementation. The policy surface (`identity_patterns`, `allow_unsigned_for`, `AllowUnsigned` flag) is already enforced — what's missing is the cryptographic math. This PRD lands that math, flips `verify.IsProduction()` to `true`, drops the `samuel doctor` stub-advisory line, and tags **v2.1.0**.

The wire format, lockfile schema, and CLI surface are stable across the v2.0 → v2.1 transition (per the rc cycle's design): no user-facing migration. Plugins signed against the test registry today verify against the same identity patterns after upgrade.

## Problem statement

v2.0.0 shipped with `verify.StubVerifier` as the default `Verifier` implementation. It enforces every policy field defined in `[security]` config — pattern matching, allowlists, cache keys — but does not execute cryptographic verification. The CLI prints `signature: verified (...)` for any artifact whose policy check passes, regardless of whether a real signature exists.

This is honest at the doctor level (the rc.10 advisory line surfaces the stub state) but actively misleading on individual commands. A user reading `samuel install foo` output sees "verified" and reasonably believes the plugin's signature was cryptographically validated. It was not.

The fix is small in surface area but high in trust value: import `sigstore-go`, implement `Verify(...)` against real signatures, gate the production path behind a build tag or runtime check so tests can keep using `StubVerifier`. Once `IsProduction()` returns `true`, the stub-advisory disappears and the project crosses the line from "claims signature verification" to "performs signature verification."

## Goals

- `internal/plugin/verify/` ships a `SigstoreVerifier` implementation backed by `sigstore-go`.
- Verification covers both artifact kinds:
  - **Skill / WASM archives**: `cosign verify-blob`-equivalent via sigstore-go's blob-signature primitives.
  - **OCI image digests**: `cosign verify`-equivalent via sigstore-go's container-signature primitives.
- TUF trust root bootstrapped from `https://tuf-repo-cdn.sigstore.dev` (per the inline doc comment in `verify.go`).
- `verify.IsProduction()` returns `true` when the production verifier is wired (no build tag) and `false` otherwise.
- `samuel doctor` drops the "signature verification stub" advisory line when running with the production verifier.
- Default factory (`verify.Default()`) returns the `SigstoreVerifier` in production builds; `StubVerifier` remains available for tests and `samuel install --allow-unsigned` semantics.
- Existing test corpus (`verify_test.go`) passes against both implementations with a `t.Run("stub")` / `t.Run("sigstore")` matrix.
- Signed-fixture extension to the live registry (PRD 0007's `samuel-test-registry`) so the live tier exercises real signatures.
- RFD 0009 — Plugin signing via Sigstore enforcement (port from inline notes + this PRD's decision record).

## Non-goals

- No change to the policy surface in `samuel.toml` (`[security]` block). Schema is stable.
- No removal of `--allow-unsigned`. The flag still works; it just now has cryptographic teeth on the default path.
- No reissuing of existing signed plugins. Identity patterns and Rekor entries created during v2.0 testing remain valid.
- No TUF root rotation tooling. Bootstrap from upstream sigstore TUF root; rotation is a future-PRD problem.
- No support for self-hosted Rekor / Fulcio. Public sigstore infrastructure only for v2.1.
- No `samuel sign` command. Plugin authors continue to use `cosign sign-blob` directly. A `samuel publish` wrapper might add this later.

## Requirements

### Functional

1. **`SigstoreVerifier` implementation** at `internal/plugin/verify/sigstore.go`:
   - Imports `github.com/sigstore/sigstore-go`.
   - Implements the existing `Verifier` interface (do not change the interface signature).
   - Constructor `NewSigstoreVerifier(policy Policy, opts ...Option)` returns `*SigstoreVerifier`.
   - Trust root loaded lazily from TUF on first verify call; cached in `~/.samuel/cache/sigstore/trust-root/` keyed by binary version (mirrors the existing cache pattern).
   - Verification result types match `StubVerifier`'s (`Result{Verified, Identity, Reason, ...}`), so calling code is unchanged.
   - One verify path per artifact kind:
     - `verifyBlob(ctx, digest, signatureBundle, identityPatterns)` for skill / WASM archives.
     - `verifyOCI(ctx, imageDigest, identityPatterns)` for OCI plugins.
   - Identity-pattern matching reuses the existing `verify.matchPattern` helper (already tested in `verify_test.go`).

2. **`Default()` factory update** at `internal/plugin/verify/verify.go`:
   - Returns `NewSigstoreVerifier(policy)` by default.
   - Honors `SAMUEL_VERIFY_STUB=1` env var → returns `StubVerifier` (test escape hatch).
   - `IsProduction()` checks the type of the `Default()` return and reports `true` for `*SigstoreVerifier`.

3. **Doctor advisory removal**:
   - `internal/commands/doctor.go` (or wherever the advisory line is emitted) drops the "verifier is a policy-only stub" line when `verify.IsProduction()` returns `true`.
   - Replace with a one-line confirmation: `signature verifier: sigstore-go (production)`.

4. **Signed live-registry fixtures**:
   - Extend `samuel-test-registry` (PRD 0007) with signed variants:
     - `samuel-test-skill-signed` — signed against `https://github.com/samuelpkg/samuel-test-registry/*` identity pattern.
     - `samuel-test-skill-unsigned` — explicitly unsigned; tests assert install fails without `--allow-unsigned`.
     - `samuel-test-skill-wrong-identity` — signed against a non-matching identity; tests assert verify rejects.
   - Signing performed via `cosign sign-blob` in the test registry's GitHub Actions release flow.

5. **Test additions**:
   - `internal/plugin/verify/sigstore_test.go` — unit tests using sigstore-go's test helpers + recorded Rekor responses. No network in unit tests.
   - `e2e/live/verify_live_test.go`:
     - `TestVerify_SignedFixture_Verifies` — install the signed fixture; verify result reports `Verified: true`.
     - `TestVerify_UnsignedFixture_RejectsWithoutFlag` — install without `--allow-unsigned`; expect error pointing to identity-pattern docs.
     - `TestVerify_UnsignedFixture_AcceptsWithFlag` — same install with `--allow-unsigned`; expect success and `Reason: --allow-unsigned`.
     - `TestVerify_WrongIdentity_Rejects` — expect verify failure citing the identity-pattern mismatch.
   - All existing `verify_test.go` tests run against both implementations via `t.Run(name, ...)` subtests.

6. **Documentation updates**:
   - `docs/concepts/signing.md` — drop the stub-disclosure paragraph; add "How sigstore-go verification works" with a sequence diagram (TUF fetch → Rekor lookup → signature math → identity match → result cache).
   - `docs/reference/cli.md` — `samuel install --help` output dropped of any stub language; `--allow-unsigned` description tightened.
   - `docs/rfd/0009.md` — new RFD: "Plugin signing via Sigstore enforcement (v2.1)."
   - `CHANGELOG.md` — `## [v2.1.0]` entry highlights the math swap as the headline change.

7. **Registry index extension**:
   - Document the existing `signature_bundle` URL field in `index.toml` (was already nullable in v2.0).
   - For v2.1 registry indexes, plugins SHOULD include the bundle URL; verify code falls back to `--allow-unsigned` flow if missing.

8. **Honest CLI output**:
   - `samuel install foo` success line now prints actual identity:
     `installed foo@1.0.0 (signed by https://github.com/samuelpkg/samuel-test-registry/release.yml@refs/tags/v1.0.0)`
   - Failure includes Rekor log entry URL for debuggability:
     `verify failed: identity https://github.com/wrong/wrong did not match any pattern (rekor: https://rekor.sigstore.dev/api/v1/log/entries/...)`

### Non-functional

- Cold verify (no cache hit) wall time ≤ 3s on a reasonable laptop. Includes TUF fetch on first call; subsequent calls ≤ 500ms.
- Verify cache hit ≤ 50ms.
- TUF fetch retries 3x with backoff on transient network failure; surfaces a structured error after the third.
- No new top-level dependency footprint beyond `sigstore-go` and its transitive deps. Audit transitively; reject any GPL-licensed transitive dep.
- All structured errors from the verify package include `DocsURL` pointing at `docs/concepts/signing.md`.
- Sigstore-go pinned by exact version in `go.mod`; pin bump goes through a PR with a security review.

## Acceptance criteria

- [ ] `SigstoreVerifier` exists, compiles, implements the `Verifier` interface.
- [ ] `verify.Default()` returns `*SigstoreVerifier` by default; `*StubVerifier` only when `SAMUEL_VERIFY_STUB=1`.
- [ ] `verify.IsProduction()` returns `true` in default builds.
- [ ] `samuel doctor` output no longer contains the string `policy-only stub` in default-build runs; instead shows `signature verifier: sigstore-go (production)`.
- [ ] `samuel-test-registry` carries the three signing fixtures (signed, unsigned, wrong-identity).
- [ ] `go test ./internal/plugin/verify/... -count=1 -v` passes for both implementations.
- [ ] `go test -tags e2e_live ./e2e/live/... -run TestVerify -v` passes against the signed fixtures.
- [ ] Cold-verify wall time on reference laptop ≤ 3s; cache-hit ≤ 50ms (measured by a benchmark in `verify_bench_test.go`).
- [ ] `docs/rfd/0009.md` is committed and rendered in mkdocs.
- [ ] `CHANGELOG.md` v2.1.0 entry committed with the math-swap as the lede.
- [ ] `samuel install foo` against a signed plugin prints the actual signing identity (no placeholder).
- [ ] v2.1.0-rc.1 tag → goreleaser publishes signed artifacts (self-verifying: v2.1.0 binary verifies its own checksum bundle).
- [ ] After 1 week soak: v2.1.0 tag; announcement posted.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| `sigstore-go` API surface changes between pin times | Medium | Pin exact version; subscribe to sigstore-go releases; review each bump in a security-focused PR |
| TUF fetch fails behind corporate proxies / air-gapped envs | High | Document `SAMUEL_VERIFY_STUB=1` as the supported escape hatch; add `SAMUEL_TUF_MIRROR` env for self-hosted mirrors (future PRD) |
| First-run latency surprises users | Medium | TUF fetch progress surfaced via spinner; documented in docs/concepts/signing.md; cache aggressively |
| Existing signed plugins (if any) signed with non-conforming identities | Low | Audit before launch; either re-sign with conforming identity or extend default identity_patterns |
| Misleading "verified" output during stub-mode fallback | Medium | When stub is active, output prints `signature verifier: stub (test mode)` even on single commands, not just doctor |
| Test fixture private signing keys leak via GitHub Actions | Low | Use OIDC-based keyless signing in fixture release flow; no long-lived signing keys |
| sigstore-go transitive dep license surprise | Low | License audit step in CI; fail on GPL/AGPL transitive |

## Open questions

- **Trust root caching duration**: 24h TTL (sigstore default) vs samuel-specific override? Recommend 24h matching upstream.
- **Offline verify mode**: should `samuel install --offline` work for cached-verify hits? Recommend yes; document the staleness window.
- **Self-hosted Rekor / Fulcio support**: defer to v2.2 with explicit `SAMUEL_REKOR_URL` / `SAMUEL_FULCIO_URL` env vars. Out of scope here.
- **`samuel sign` wrapper**: not in this PRD. Plugin authors use `cosign sign-blob` directly. Revisit when plugin-authoring CLI gets prioritized.
- **Trust root rotation**: punted to a future PRD. Document upstream sigstore's rotation policy in docs/concepts/signing.md so users understand the lifecycle.

## Task hints

1. Add `sigstore-go` to `go.mod`; run license audit
2. Scaffold `internal/plugin/verify/sigstore.go` with the interface impl skeleton
3. Implement `verifyBlob` against a hand-crafted Rekor response fixture
4. Implement `verifyOCI` against a public sigstore-signed image (e.g. cosign's own release image)
5. Wire the TUF trust-root fetch + cache to `~/.samuel/cache/sigstore/`
6. Update `verify.Default()` factory; add `SAMUEL_VERIFY_STUB` env handling
7. Update `verify.IsProduction()` to type-check the default
8. Run existing `verify_test.go` against both impls via subtests; fix any drift
9. Write `sigstore_test.go` with recorded Rekor fixtures
10. Add signed/unsigned/wrong-identity fixture plugins to `samuel-test-registry`
11. Write the test registry's release-flow GitHub Action that signs fixtures via cosign + OIDC
12. Write `e2e/live/verify_live_test.go`
13. Update `samuel doctor` advisory logic to suppress stub-line when production
14. Update `samuel install` output to print signing identity from the Result
15. Add `verify_bench_test.go` for cold-verify and cache-hit benchmarks
16. Update `docs/concepts/signing.md` with sequence diagram + drop stub paragraph
17. Update `docs/reference/cli.md`
18. Draft `docs/rfd/0009.md` — Plugin signing via Sigstore enforcement
19. Update `rfd-index.toml`
20. Write CHANGELOG v2.1.0 entry
21. Tag v2.1.0-rc.1; smoke test
22. After 1 week soak: tag v2.1.0; announce
