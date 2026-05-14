---
name: samuel-test-skill-with-git
description: Live e2e fixture — exercises rc.9, the .git strip after clone in source.fetchGit.
---

# samuel-test-skill-with-git

Every git clone leaves a `.git/` directory in the worktree. Samuel's
fetcher removes it before returning, because Samuel resolves plugins by
name + lockfile digest, not by walking commit history. The rc.9
regression was that the strip was skipped on certain code paths; this
fixture asserts the installed tree has no `.git/`.

No special repo setup is required to publish this fixture — every git
repo has `.git/` after clone. The test asserts the install step strips it.
