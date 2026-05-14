---
name: samuel-test-skill-signed
description: Live-e2e fixture verifying the signed-install path. Asserts that an artifact signed against the samuel-test-registry identity_pattern passes verification under the production sigstore verifier.
---

# samuel-test-skill-signed

Used by `e2e/live/verify_live_test.go::TestVerify_SignedFixture_Verifies`.

The release workflow signs `SKILL.md` with `cosign sign-blob --bundle`
under an OIDC identity matching
`https://github.com/samuelpkg/samuel-test-registry/*`. The framework's
default `identity_patterns` accepts that range, so verify reports
`Verified: true` with the identity URL surfaced in the install line.
