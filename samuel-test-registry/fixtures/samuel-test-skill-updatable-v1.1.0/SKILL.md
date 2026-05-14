---
name: samuel-test-skill-updatable
description: Live e2e fixture — version 1.1.0 of the updatable skill. Used to verify `samuel update` bumps lockfile + plugin tree.
---

# samuel-test-skill-updatable (1.1.0)

Second version. The live e2e `TestUpdate_LiveRegistry_BumpsVersion`
test starts at 1.0.0, runs `samuel update`, and asserts the lockfile
reflects this version.
