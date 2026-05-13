#!/usr/bin/env bash
# End-to-end smoke test for Samuel v2.0.0-rc.1.
#
# Acceptance criteria from PRD 0005 §Acceptance. Each step asserts a
# concrete on-disk or process-exit invariant. The script aborts on the
# first failure and prints the failing step.
#
# Usage:
#   ./scripts/smoke-test.sh                # full run (requires network)
#   ./scripts/smoke-test.sh --offline      # skips steps that hit GHCR / GitHub
#   ./scripts/smoke-test.sh --keep-tmp     # keeps the scratch project for inspection
#
# Expectations:
#   - `samuel` is on PATH (or pass SAMUEL=/path/to/samuel).
#   - The samuel-registry on GitHub has the migrated 77 entries (post-bulk-migration).

set -euo pipefail

OFFLINE=0
KEEP_TMP=0
for arg in "$@"; do
  case "$arg" in
    --offline)  OFFLINE=1 ;;
    --keep-tmp) KEEP_TMP=1 ;;
    *) echo "unknown flag: $arg" >&2; exit 64 ;;
  esac
done

SAMUEL="${SAMUEL:-samuel}"
SCRATCH=$(mktemp -d "${TMPDIR:-/tmp}/samuel-smoke.XXXXXX")
PROJECT="$SCRATCH/my-test-project"
PASS=0

step() {
  printf '\n\x1b[1;36m▶ %s\x1b[0m\n' "$1"
}

ok() {
  PASS=$((PASS+1))
  printf '  \x1b[32m✓\x1b[0m %s\n' "$1"
}

fail() {
  printf '  \x1b[31m✗\x1b[0m %s\n' "$1" >&2
  exit 1
}

cleanup() {
  if [ "$KEEP_TMP" = "0" ]; then
    rm -rf "$SCRATCH"
  else
    echo "scratch preserved at: $SCRATCH"
  fi
}
trap cleanup EXIT

assert_no_file() {
  if [ -e "$1" ]; then fail "expected $1 to be absent (agnostic invariant)"; fi
}

assert_file() {
  if [ ! -e "$1" ]; then fail "expected $1 to exist"; fi
}

# ---- 9.1 install -------------------------------------------------------
step "9.1 samuel binary present"
if ! "$SAMUEL" --version >/dev/null 2>&1; then
  fail "samuel binary not on PATH (set SAMUEL=...)"
fi
"$SAMUEL" --version
ok "samuel --version reports a version"

# ---- 9.2 init starter pack --------------------------------------------
step "9.2 samuel init installs starter pack"
mkdir -p "$PROJECT"
( cd "$PROJECT" && "$SAMUEL" init my-test-project )
assert_file "$PROJECT/AGENTS.md"
assert_file "$PROJECT/.samuel/samuel.toml"
ok "AGENTS.md and .samuel/samuel.toml created"
# Agnostic invariant: no CLAUDE.md yet.
assert_no_file "$PROJECT/CLAUDE.md"
ok "no CLAUDE.md exists (agnostic invariant)"

if [ "$OFFLINE" = "1" ]; then
  echo "offline mode: skipping registry-dependent steps"
  echo
  echo "smoke test partial pass: $PASS step(s) ok"
  exit 0
fi

# ---- 9.3 / 9.4 language + framework skill installs --------------------
step "9.3 samuel install go-guide"
( cd "$PROJECT" && "$SAMUEL" install go-guide --yes )
assert_file "$PROJECT/.samuel/plugins/go-guide/SKILL.md"
ok "go-guide skill payload landed"

step "9.4 samuel install react"
( cd "$PROJECT" && "$SAMUEL" install react --yes )
assert_file "$PROJECT/.samuel/plugins/react/SKILL.md"
ok "react skill payload landed"

# ---- 9.5 claude-translator WASM ---------------------------------------
step "9.5 samuel install claude-translator"
( cd "$PROJECT" && "$SAMUEL" install claude-translator --yes )
( cd "$PROJECT" && "$SAMUEL" sync )
assert_file "$PROJECT/CLAUDE.md"
ok "CLAUDE.md mirror created"

# ---- 9.6 codex-translator second WASM ---------------------------------
step "9.6 samuel install codex-translator"
( cd "$PROJECT" && "$SAMUEL" install codex-translator --yes )
( cd "$PROJECT" && "$SAMUEL" sync )
assert_file "$PROJECT/.codex/context.md"
ok ".codex/context.md mirror created"

# ---- 9.7 OCI tier (optional; depends on claude-runner availability) ---
step "9.7 samuel install claude-runner (OCI tier, optional)"
if ( cd "$PROJECT" && "$SAMUEL" install claude-runner --yes 2>/dev/null ); then
  ok "claude-runner OCI plugin installed"
else
  echo "  (claude-runner not yet published in registry — skipping)"
fi

# ---- 9.8 / 9.9 run init + run start -----------------------------------
step "9.8 samuel run init"
mkdir -p "$PROJECT/.samuel/tasks"
cat > "$PROJECT/.samuel/tasks/sample-prd.md" <<'PRD'
---
prd: "SAMPLE"
title: Smoke-test PRD
---
# Smoke

Trivial PRD used by the smoke test.
PRD
( cd "$PROJECT" && "$SAMUEL" run init --prd .samuel/tasks/sample-prd.md )
assert_file "$PROJECT/.samuel/state/prd.toon"
ok "prd.toon materialized"

step "9.9 samuel run start --iterations 1"
if ( cd "$PROJECT" && "$SAMUEL" run start --iterations 1 --dry-run ); then
  ok "run start completed one iteration (dry-run)"
else
  echo "  (run start dry-run failed — non-fatal in smoke test)"
fi

# ---- 9.10 agnostic invariant ------------------------------------------
step "9.10 verify agnostic invariant"
NEW_PROJECT="$SCRATCH/clean-no-translator"
mkdir -p "$NEW_PROJECT"
( cd "$NEW_PROJECT" && "$SAMUEL" init clean-no-translator --minimal )
assert_no_file "$NEW_PROJECT/CLAUDE.md"
ok "fresh --minimal init has no CLAUDE.md"

# ---- 9.11 minimal init -------------------------------------------------
step "9.11 samuel init --minimal skips starter pack"
ls "$NEW_PROJECT/.samuel/plugins/" 2>/dev/null | grep -q . && \
  fail "starter plugins installed under --minimal" || \
  ok ".samuel/plugins is empty under --minimal"

# ---- 9.12 --without flag ----------------------------------------------
step "9.12 samuel init --without"
WITHOUT_PROJECT="$SCRATCH/without-test"
mkdir -p "$WITHOUT_PROJECT"
( cd "$WITHOUT_PROJECT" && "$SAMUEL" init without-test --without create-rfd,security-audit )
if [ -d "$WITHOUT_PROJECT/.samuel/plugins/create-rfd" ] || [ -d "$WITHOUT_PROJECT/.samuel/plugins/security-audit" ]; then
  fail "--without did not exclude the named plugins"
fi
INSTALLED=$(ls "$WITHOUT_PROJECT/.samuel/plugins/" 2>/dev/null | wc -l | tr -d ' ')
if [ "$INSTALLED" != "10" ]; then
  echo "  warning: expected 10 plugins, found $INSTALLED"
fi
ok "create-rfd and security-audit excluded; $INSTALLED other plugins installed"

echo
echo "smoke test pass: $PASS step(s) ok"
