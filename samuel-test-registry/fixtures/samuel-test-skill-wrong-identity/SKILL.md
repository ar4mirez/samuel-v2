---
name: samuel-test-skill-wrong-identity
description: Live-e2e fixture verifying the identity-mismatch rejection path. A real signature exists but the OIDC subject does not match the default identity_patterns; verify must reject with a structured error citing the mismatch.
---

# samuel-test-skill-wrong-identity

Used by `e2e/live/verify_live_test.go::TestVerify_WrongIdentity_Rejects`.

The release workflow signs `SKILL.md` from a fixture repo whose OIDC
subject is `https://github.com/samuelpkg-fixtures/wrong-identity/*` —
explicitly outside the default `identity_patterns` allowlist. The
sigstore verifier evaluates each pattern as a SAN regex; none match,
so the install fails with a structured error pointing at the
identity-pattern docs.
