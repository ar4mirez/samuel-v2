# samuel-test-registry (source-of-truth)

> **Internal use only — do not depend on these plugins.**
> The registry index in this directory is the canonical source for the public
> [`github.com/samuelpkg/samuel-test-registry`](https://github.com/samuelpkg/samuel-test-registry)
> repo that backs the `e2e/live/` nightly test tier. Schema may change with
> any framework version — there are no public stability guarantees.

## Why this exists

The hermetic e2e tier ([`e2e/hermetic/`](../e2e/hermetic)) drives the
`samuel` CLI against a local `file://` registry. That covers ~80% of the
manual sweep but routes through `source.fetchFile` rather than
`source.fetchGit`. The git-fetcher codepath — and the two bugs we fixed in
it during the rc-cycle (rc.6 `v`-prefix tag fallback, rc.9 `.git` strip on
install) — has no CLI-surface coverage without a real git registry.

`samuel-test-registry/` is the *content* for that registry, kept in the
framework repo so reviewers can see schema + fixture changes alongside any
code change that affects them. The actual `git push` to the external
`samuelpkg/samuel-test-registry` repo is a manual step (see _Publishing_
below).

## Layout

```
samuel-test-registry/
├── README.md                   # this file
├── index.toml                  # registry index; points at fixture plugin repos
├── .github/workflows/
│   └── sign-fixtures.yml       # PRD 0008: keyless cosign sign-blob on tag push
└── fixtures/                   # one directory per fixture plugin
    ├── samuel-test-skill-basic/             # minimal SKILL.md fixture
    ├── samuel-test-skill-tagged-v/          # release tagged v1.0.0 (rc.6 fixture)
    ├── samuel-test-skill-tagged-bare/       # release tagged 1.0.0 (rc.6 fallback)
    ├── samuel-test-skill-with-git/          # `.git/` left after clone (rc.9 fixture)
    ├── samuel-test-skill-updatable-v1.0.0/  # snapshot at 1.0.0
    ├── samuel-test-skill-updatable-v1.1.0/  # snapshot at 1.1.0
    ├── samuel-test-skill-signed/            # PRD 0008: signed against samuel-test-registry/* identity
    ├── samuel-test-skill-unsigned/          # PRD 0008: no bundle — verify fails closed
    └── samuel-test-skill-wrong-identity/    # PRD 0008: real signature, identity outside allowlist
```

Each fixture directory contains a `samuel-plugin.toml` + `SKILL.md` + (in
some cases) one reference file. Plugins are intentionally minimal — each
is ≤10 KB so clone time is negligible.

## Fixture matrix

| Fixture | Tag scheme | Coverage |
|---|---|---|
| `samuel-test-skill-basic` | `v1.0.0` | Happy path — install + manifest + lockfile |
| `samuel-test-skill-tagged-v` | `v1.0.0` | rc.6: registry says `1.0.0`, repo tags `v1.0.0` |
| `samuel-test-skill-tagged-bare` | `1.0.0` | rc.6 fallback: registry says `1.0.0`, repo tags `1.0.0` |
| `samuel-test-skill-with-git` | `v1.0.0` | rc.9: ensure `.git/` is stripped on install |
| `samuel-test-skill-updatable` | `v1.0.0`, `v1.1.0` | update path: 1.0.0 → 1.1.0 |
| `samuel-test-skill-signed` | `v1.0.0` | PRD 0008: cosign-signed SKILL.md.bundle, OIDC subject matches `identity_patterns` |
| `samuel-test-skill-unsigned` | `v1.0.0` | PRD 0008: no signature_bundle published — install fails without `--allow-unsigned` |
| `samuel-test-skill-wrong-identity` | `v1.0.0` | PRD 0008: real bundle, OIDC subject outside `identity_patterns` — verify rejects |

## v2.1 expectation: signature_bundle

For v2.1+ the production sigstore verifier requires every signed plugin
to publish a `signature_bundle` URL in `index.toml`. The field is a
sibling JSON file produced by `cosign sign-blob --bundle`. Fixtures
that intentionally omit the field (the `samuel-test-skill-unsigned`
row above) test the fail-closed path.

A reusable GitHub Actions workflow ([`.github/workflows/sign-fixtures.yml`](.github/workflows/sign-fixtures.yml))
runs on each fixture-repo tag push and uses keyless cosign + OIDC
(no long-lived signing keys) to attach the `.bundle` as a release
asset.

## Manual signature verification (sanity-check)

After a fresh publish, verify each signed fixture's signature locally
with stock `cosign`:

```bash
# Pull the SKILL.md + .bundle from the release.
gh release download v1.0.0 -R samuelpkg/samuel-test-skill-signed \
    -p SKILL.md -p SKILL.md.bundle -D /tmp/sig-check

# Verify the bundle (keyless — OIDC identity matched against the
# samuel-test-registry/* pattern).
cosign verify-blob \
    --bundle /tmp/sig-check/SKILL.md.bundle \
    --certificate-identity-regexp 'https://github\.com/samuelpkg/samuel-test-skill-signed/.*' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    /tmp/sig-check/SKILL.md
```

## Publishing (manual, one-time + on fixture changes)

The framework repo holds the source-of-truth tree; the publish step pushes
each fixture as its own external repo under `samuelpkg/`.

```bash
# 1. Publish the index (registry repo).
gh repo create samuelpkg/samuel-test-registry --public \
    --description "Internal-only test fixtures for Samuel's e2e/live tier"
git -C samuel-test-registry init
cp index.toml samuel-test-registry/   # already in place
git -C samuel-test-registry add . && git -C samuel-test-registry commit -m "init"
git -C samuel-test-registry remote add origin git@github.com:samuelpkg/samuel-test-registry.git
git -C samuel-test-registry push -u origin main

# 2. Publish each fixture plugin as its own repo (one per row above).
#    Each `samuel-test-skill-*` dir under fixtures/ becomes one repo.
#    Tag at the version requested by the matrix.
for fixture in samuel-test-skill-basic samuel-test-skill-tagged-v \
               samuel-test-skill-tagged-bare samuel-test-skill-with-git; do
  gh repo create "samuelpkg/$fixture" --public --description "e2e/live fixture"
  ( cd "fixtures/$fixture" && git init && git add . && git commit -m "init" \
    && git tag "v1.0.0" \
    && git remote add origin "git@github.com:samuelpkg/$fixture.git" \
    && git push -u origin main --tags )
done

# 2a. samuel-test-skill-tagged-bare: re-tag without the `v` prefix.
( cd fixtures/samuel-test-skill-tagged-bare && git tag -d v1.0.0 \
  && git tag "1.0.0" && git push --tags --force )

# 2b. samuel-test-skill-with-git is published with `.git/` retained in
#     the *cloned* tree by the registry's fetcher — no special publish
#     step. The rc.9 test asserts `samuel install` strips it locally.

# 3. Publish the updatable fixture with two tagged commits.
gh repo create samuelpkg/samuel-test-skill-updatable --public \
    --description "e2e/live update-path fixture"
mkdir /tmp/samuel-updatable && cd /tmp/samuel-updatable && git init
cp -r ${OLDPWD}/fixtures/samuel-test-skill-updatable-v1.0.0/. .
git add . && git commit -m "1.0.0" && git tag v1.0.0
rm -rf ./*
cp -r ${OLDPWD}/fixtures/samuel-test-skill-updatable-v1.1.0/. .
git add . && git commit -m "1.1.0" && git tag v1.1.0
git remote add origin git@github.com:samuelpkg/samuel-test-skill-updatable.git
git push -u origin main --tags
```

## Schema invariants

`index.toml` follows the production registry schema declared in
[`internal/plugin/registry/registry.go`](../internal/plugin/registry/registry.go):

- `schema_version = 1` — pinned; bump only when the framework parser does.
- Array-of-tables `[[plugins]]` shape — matches the official registry generator.
- Each entry carries: `name`, `repo`, `latest`, `versions`, `description`,
  `categories`, `tags`, `kind`.

If you change the schema in the framework, change it here too in the same
PR, and re-publish.
