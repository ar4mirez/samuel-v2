---
name: samuel-test-skill-tagged-v
description: Live e2e fixture — release tagged v1.0.0. Used to verify the rc.6 v-prefix tag fallback in source.fetchGit.
---

# samuel-test-skill-tagged-v

The registry index publishes this plugin as `latest = "1.0.0"` (bare
semver). The repository tags the release as `v1.0.0`. Without rc.6's
v-prefix retry in `source.fetchGit`, `samuel install` would fail with
"remote branch 1.0.0 not found." This fixture proves the fallback works
end-to-end.
