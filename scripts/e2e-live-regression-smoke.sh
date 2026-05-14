#!/usr/bin/env bash
# Smoke-test the e2e-live nightly auto-issue flow by deliberately
# breaking the rc.6 v-prefix fallback in source.fetchGit on a throwaway
# branch, running the live workflow once, and asserting that the
# nightly opens an issue named "[e2e-live] TestInstall_VPrefixedTag_Fetches
# failing." Then revert and re-run to confirm auto-close.
#
# Why this exists: PRD 0007 task 8.0 (and acceptance criterion #6)
# requires that an induced rc.6-style regression actually trips the
# nightly within one cycle. This script encodes that check so the
# smoke test is reproducible — not a one-off ceremony.
#
# Usage:
#   scripts/e2e-live-regression-smoke.sh open    # induce regression + open issue
#   scripts/e2e-live-regression-smoke.sh close   # revert + confirm auto-close
#
# Pre-reqs: gh CLI authenticated; a clean working tree on a throwaway
# branch (the script refuses to run on main).

set -euo pipefail

mode="${1:-}"
if [[ "${mode}" != "open" && "${mode}" != "close" ]]; then
  echo "usage: $0 open|close" >&2
  exit 64
fi

branch=$(git rev-parse --abbrev-ref HEAD)
if [[ "${branch}" == "main" || "${branch}" == "master" ]]; then
  echo "refusing to run on ${branch}; cut a throwaway branch first" >&2
  exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
  echo "working tree is dirty; commit or stash first" >&2
  exit 1
fi

source_file="internal/plugin/source/source.go"

case "${mode}" in
  open)
    # Force vPrefixedSemver to always return "" so the v-prefix retry
    # never kicks in. That reproduces rc.6 exactly: registry asks for
    # "1.0.0", repo only has "v1.0.0", first clone fails, no retry,
    # error surfaces to the user.
    echo "[smoke] inducing regression: disabling v-prefix fallback…"
    perl -0777 -i -pe 's/(func vPrefixedSemver\(ref string\) string \{)/$1\n\treturn "" \/\/ SMOKE TEST: rc.6 regression injected by scripts\/e2e-live-regression-smoke.sh/' "${source_file}"

    if ! grep -q "SMOKE TEST: rc.6 regression injected" "${source_file}"; then
      echo "[smoke] failed to inject regression — check perl regex against current ${source_file}" >&2
      exit 2
    fi

    git add "${source_file}"
    git commit -m "smoke(e2e-live): induce rc.6 regression for nightly auto-issue test"
    git push origin "${branch}"

    echo
    echo "[smoke] pushed regression. Trigger the workflow:"
    echo "  gh workflow run e2e-live.yml --ref ${branch}"
    echo
    echo "After it fails, verify auto-issue exists:"
    echo "  gh issue list --label e2e-live-red --state open"
    echo "  (look for: [e2e-live] TestInstall_VPrefixedTag_Fetches failing)"
    echo
    echo "When done, run: $0 close"
    ;;

  close)
    echo "[smoke] reverting regression…"
    perl -0777 -i -pe 's/\n\treturn "" \/\/ SMOKE TEST: rc.6 regression injected by scripts\/e2e-live-regression-smoke.sh//' "${source_file}"

    if grep -q "SMOKE TEST: rc.6 regression injected" "${source_file}"; then
      echo "[smoke] revert did not remove the injected line — check perl regex" >&2
      exit 2
    fi

    git add "${source_file}"
    git commit -m "smoke(e2e-live): revert rc.6 regression — verify auto-close"
    git push origin "${branch}"

    echo
    echo "[smoke] pushed revert. Re-trigger the workflow:"
    echo "  gh workflow run e2e-live.yml --ref ${branch}"
    echo
    echo "After it succeeds, the prior issue should auto-close. Verify:"
    echo "  gh issue list --label e2e-live-red --state closed --limit 5"
    ;;
esac
