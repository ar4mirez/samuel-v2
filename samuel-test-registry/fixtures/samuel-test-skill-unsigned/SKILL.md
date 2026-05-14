---
name: samuel-test-skill-unsigned
description: Live-e2e fixture verifying the fail-closed path for an artifact with no signature_bundle. The default policy demands a signature; install must reject unless --allow-unsigned is set.
---

# samuel-test-skill-unsigned

Used by `e2e/live/verify_live_test.go::TestVerify_UnsignedFixture_RejectsWithoutFlag`
and `TestVerify_UnsignedFixture_AcceptsWithFlag`.

The fixture publishes no `signature_bundle` URL in the registry index.
A clean `samuel install samuel-test-skill-unsigned` must produce a
structured error citing the docs page. The same install with
`--allow-unsigned` succeeds and records `Reason: --allow-unsigned` in
the lockfile.
