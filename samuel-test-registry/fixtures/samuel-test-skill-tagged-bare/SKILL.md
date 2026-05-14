---
name: samuel-test-skill-tagged-bare
description: Live e2e fixture — release tagged 1.0.0 (no v). Sanity-check that bare-semver tags still resolve without the v-prefix fallback kicking in.
---

# samuel-test-skill-tagged-bare

Counterpart to `samuel-test-skill-tagged-v`. The registry publishes
`latest = "1.0.0"` and the repo tags the release as bare `1.0.0`. This
fixture asserts that the *first* clone attempt succeeds without falling
through to the v-prefix retry — i.e., we don't regress in the other
direction by always rewriting tags.
