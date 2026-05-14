# Sigstore Fixture Corpus

Recorded Rekor responses + sample bundles used by `sigstore_test.go`.

## Rotation playbook

When sigstore-go ships a breaking change to the trust-root format or the
bundle protobuf wire format, the recorded fixtures here will start
failing to parse. To refresh:

1. Pin the new sigstore-go version in `go.mod`.
2. Fetch the latest `trusted_root.json` from
   `https://tuf-repo-cdn.sigstore.dev/targets/trusted_root.json`. Commit
   it as `trusted_root.json` here.
3. Sign one of `samuel-test-skill-signed`'s artifacts with `cosign
   sign-blob --bundle` and commit the resulting `.bundle` JSON.
4. Bump the test expectations in `sigstore_test.go` if any failure
   messages or identity strings have changed shape.

## Why we record fixtures

The unit-test tier MUST be hermetic — no network. The live-tier
(`e2e/live/verify_live_test.go`) exercises real Rekor; the unit tier
proves the bundle-parsing + identity-pattern math against frozen
fixtures so unrelated PRs do not flake on Rekor's availability.
