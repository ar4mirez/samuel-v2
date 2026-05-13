#!/usr/bin/env bash
# Patch one plugin repo's .github/workflows/release.yml on GitHub, repointing
# the `uses:` to samuelpkg/samuel-plugin-release. Uses the Contents API so we
# don't need to clone the repo locally.
#
# Usage:
#   ./scripts/patch-plugin-workflow.sh samuel-go-guide
#
# Idempotent: if the workflow already references the new owner, exits clean.

set -e

repo="$1"
if [ -z "$repo" ]; then
  echo "usage: $0 <repo-name>" >&2
  exit 64
fi

owner="${OWNER:-samuelpkg}"
path=".github/workflows/release.yml"

resp=$(/opt/homebrew/bin/gh api "repos/$owner/$repo/contents/$path" 2>/dev/null || echo "")
if [ -z "$resp" ] || [[ "$resp" == *'"status":"404"'* ]]; then
  echo "  skip: $repo (no $path)"
  exit 0
fi

sha=$(printf '%s' "$resp" | jq -r '.sha')
decoded=$(printf '%s' "$resp" | jq -r '.content' | base64 -d 2>/dev/null)

if [[ "$decoded" != *"ar4mirez/samuel-plugin-release"* ]]; then
  echo "  skip: $repo (already on samuelpkg or unrecognized format)"
  exit 0
fi

patched=$(printf '%s' "$decoded" | sed 's|ar4mirez/samuel-plugin-release|samuelpkg/samuel-plugin-release|g')
new_b64=$(printf '%s' "$patched" | base64 | tr -d '\n')

/opt/homebrew/bin/gh api -X PUT "repos/$owner/$repo/contents/$path" \
  -f message="chore: repoint release workflow to samuelpkg/samuel-plugin-release" \
  -f content="$new_b64" \
  -f sha="$sha" \
  --jq '"  patched: \(.content.path) @ \(.commit.sha[0:7])"' 2>&1 | tail -2
