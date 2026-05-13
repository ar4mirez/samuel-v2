---
name: sample-skill
description: A minimal skill fixture used by the hermetic e2e test suite. Not a real plugin; do not publish.
---

# sample-skill

Used by `e2e/hermetic/*_test.go` to exercise the install pipeline against
a known-good payload. The content of this file is intentionally inert —
the e2e suite only asserts on filesystem presence, manifest parsing, and
samuel.lock state, not on what the skill itself "does".
