#!/usr/bin/env bash
# Reconcile a samuelpkg/samuel-<name> repo with the canonical content
# produced by scripts/migrate-v1-skills. Used to recover from the
# Milestone-5 push bug that dropped some plugins' .github/ and
# references/ dirs on the initial bulk push.
#
# Strategy:
#   1. Clone the live repo into a sibling worktree.
#   2. Mirror the generated tree on top, removing any stale files.
#   3. If there's any drift, commit + push. Otherwise skip.
#
# Usage:
#   SAMUEL_V2=/path/to/samuel_v2 ./scripts/sync-plugin-content.sh samuel-vapor

set -e

repo="$1"
if [ -z "$repo" ]; then
  echo "usage: $0 <repo-name>" >&2
  exit 64
fi

owner="${OWNER:-samuelpkg}"
v2="${SAMUEL_V2:-$(pwd)}"
src="$v2/migration-output/$repo"

if [ ! -d "$src" ]; then
  echo "  skip: $repo (no local source at $src)" >&2
  exit 0
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/samuel-sync.XXXXXX")
trap 'rm -rf "$work"' EXIT

git clone --quiet "git@github.com:$owner/$repo.git" "$work/$repo"
cd "$work/$repo"

# Wipe the tracked tree, then mirror the canonical source on top.
# This guarantees deletions land too (a file in remote but not local).
git ls-files -z | xargs -0 -r rm -f
# Preserve .git/, copy everything else from src.
(cd "$src" && tar -cf - --exclude=.git . ) | tar -xf -

git add -A
if git diff --staged --quiet; then
  echo "  skip: $repo (no drift)"
  exit 0
fi

git -c user.email="${GIT_AUTHOR_EMAIL:-angel@cuemby.com}" \
    -c user.name="${GIT_AUTHOR_NAME:-Angel Ramirez}" \
  commit --quiet -m "chore: restore content dropped by initial bulk push

Reconciles the live repo against the canonical scripts/migrate-v1-skills
output. Restores .github/workflows/release.yml and any references/,
scripts/, or assets/ subdirectories that didn't make it into the
initial v1.0.0 commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
git push --quiet origin main
echo "  synced: $repo"
