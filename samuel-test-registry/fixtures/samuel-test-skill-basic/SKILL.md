---
name: samuel-test-skill-basic
description: Minimal skill fixture for the live e2e test tier. Not a real plugin; do not publish externally beyond samuelpkg/.
---

# samuel-test-skill-basic

Used by `e2e/live/*_test.go` to exercise the happy path: registry resolve
→ git clone → install → manifest parse → lockfile write. Content is
intentionally inert.
