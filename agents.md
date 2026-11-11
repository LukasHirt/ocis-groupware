# agents.md — ocis-groupware

## Repository Overview

This repository builds the official **oCIS Groupware** Docker image
(`owncloud/ocis-groupware` on Docker Hub) — calendar and contacts for ownCloud Infinite
Scale. It does not contain the ocis-groupware source code — it builds it **from source**
by cloning [`owncloud/ocis-groupware`](https://github.com/owncloud/ocis-groupware) at a
signed release tag and ships a minimal distroless runtime image. Images are
multi-architecture and built via GitHub Actions.

This repository is the **only** publisher of the image, the same split as oCIS: the
application repository publishes binaries and the GitHub release, and this repository
publishes the container image.

- **Classification:** Docker image build (from source)
- **Activity Status:** Active
- **License:** Apache-2.0
- **Language:** Dockerfile, Shell

## Architecture & Key Paths

- `Dockerfile.multiarch` — four-stage build (build context is the repo root, but nothing
  is copied from it):
  - **source** (`golang:*-bookworm`) — clones upstream at `${GIT_REF}`, pinned to
    `${GIT_SHA}` when set.
  - **web** (`node:*-bookworm-slim`) — builds the Vue web app (`npm ci && npm run build`).
  - **build** (`golang:*-bookworm`) — compiles `./cmd/ocis-groupware` with
    `CGO_ENABLED=0` for `${TARGETARCH}`, embedding `web/dist`.
  - **runtime** (`gcr.io/distroless/static-debian12:nonroot`) — `/ocis-groupware` as the
    entrypoint, `/data` owned by 65532:65532, `GROUPWARE_DATA_DIR=/data`,
    `GROUPWARE_DEBUG_ADDR=:9180`, ports `8080` and `9180`.
- `scripts/resolve-pending-releases.sh` — decides what CI builds; prints the build
  matrix as JSON.
- `scripts/verify-tag.sh` — checks that an upstream tag is annotated and its signature
  is verified, and prints the commit it points to.
- `scripts/test-resolve-pending-releases.sh` — offline tests for the resolver.
- `.github/workflows/main.yml` — **active** CI: test, resolve, build, scan, smoke test,
  publish.
- `.github/workflows/lint-pr-title.yml` — Conventional-Commit PR-title enforcement
- `.github/dependabot.yml` — weekly GitHub Actions and Docker base-image dependency updates
- `.github/CODEOWNERS` — review ownership
- `.editorconfig` — formatting rules (2-space indent, LF, trailing newline)
- `.trivyignore` — accepted-CVE exclusions for the Trivy scan
- `LICENSE` — Apache-2.0

There is **no `CHANGELOG.md`** in this repository.

## Build & CI

GitHub Actions cannot trigger on a tag pushed to another repository, so `main.yml`
polls. Its `prepare` job runs `scripts/resolve-pending-releases.sh`, which:

1. Lists the upstream tags (`git ls-remote --tags`) and keeps only plain `vX.Y.Z` ones.
2. Lists the tags already on Docker Hub (paginated). A missing Docker Hub repository
  (HTTP 404) counts as "nothing published"; any other failure aborts the run.
3. Picks candidates by `MODE`, verifies each with `scripts/verify-tag.sh`, and prints
  `[{version, ref, sha, latest}, …]`. `latest` is true only for the highest upstream
  version (`sort -V`).

| Trigger | `MODE` | Builds |
|---------|--------|--------|
| `schedule` `0 */6 * * *`, `workflow_dispatch` without `rebuild-newest` | `pending` | every release not yet on Docker Hub |
| `push` to `main`, `schedule` `0 3 * * 0`, `workflow_dispatch` with `rebuild-newest` | `rebuild` | the same, plus the newest release |
| `pull_request` | `pr` | the newest release, or upstream `main` while there is none; never pushed |

`build` calls the reusable `docker-build-native.yml` workflow from
[`owncloud-docker/ubuntu`](https://github.com/owncloud-docker/ubuntu) per matrix leg
(`fail-fast: false`). Each leg is tagged `X.Y.Z` and `X.Y.Z-YYYYMMDD`, plus `latest` for
the highest release. It is scanned with Trivy (`.trivyignore`, `exit-code: 1`) and
smoke-tested by starting the default entrypoint with
`GROUPWARE_OIDC_ISSUER=https://ocis.invalid` and polling
`http://localhost:9180/healthz`. There is no version assertion, because the binary
reports no version. On non-PR events the image is pushed to Docker Hub and the README
is synced as the image description.

The resolver's inputs can be replaced for testing: `UPSTREAM_TAGS`,
`UPSTREAM_MAIN_SHA`, `PUBLISHED_TAGS`, `DOCKERHUB_TAGS_URL`, `VERIFY_TAG`,
`CURL_RETRY` (see the script header).

```bash
scripts/test-resolve-pending-releases.sh                               # offline tests
MODE=pending scripts/resolve-pending-releases.sh                       # what CI would build now
GH_TOKEN=$(gh auth token) MODE=pr scripts/resolve-pending-releases.sh  # what a PR would build
```

To build and smoke-test locally:

```bash
docker buildx build --load -f Dockerfile.multiarch --build-arg GIT_REF=main -t ocis-groupware:test .
docker run --rm -p 9180:9180 -e GROUPWARE_OIDC_ISSUER=https://ocis.invalid ocis-groupware:test
curl -f http://localhost:9180/healthz
```

## Deviations from the Sibling Repositories

- **Distroless instead of Alpine.** The binary is static (`CGO_ENABLED=0`), and
  distroless `static` has no shell, package manager or libc. Its CVEs come almost
  entirely from the Go modules and Go standard library inside the binary. Because there
  is no `apk upgrade` to pick up OS patches at build time, a blind rebuild fixes nothing;
  fixes arrive as Dependabot digest bumps (`golang`, distroless), and merging one
  rebuilds the newest release.
- **uid 65532 instead of 1000.** Upstream's Helm chart (`runAsUser`/`fsGroup: 65532`),
  its documentation and existing `/data` volumes all assume the distroless `nonroot`
  uid.
- **Release polling instead of a hand-edited matrix.** New upstream tags are built
  automatically, as in `web-extensions`, rather than added to `main.yml` by a PR, as in
  `ocis`.
- **No rolling or nightly image.** Only signed release tags are published. Upstream
  `main` is built only in pull requests, never pushed.
- **No `vN/` directory.** Everything is 0.x and only the newest release is rebuilt, so
  there is one root `Dockerfile.multiarch` and `.trivyignore`. Add a `vN/` directory
  only when a major line needs a different build.

## Development Conventions

- **No CHANGELOG** — do not create one.
- Conventional-Commit PR titles, enforced by `lint-pr-title.yml`.
- `.editorconfig` governs formatting; `LICENSE` is exempt from the indent rules.
- GitHub Actions are pinned to full commit SHAs.
- Workflows declare a least-privilege `permissions:` block.
- Scripts must stay `shellcheck`-clean; CI runs it.
- Bug reports for the application itself go upstream to
  [`owncloud/ocis-groupware`](https://github.com/owncloud/ocis-groupware); this repo
  tracks only the Docker packaging.

## OSPO Policy Constraints

### GitHub Actions
- **Only** use actions owned by `owncloud`, created by GitHub (`actions/*`),
  verified on the GitHub Marketplace, or verified by the ownCloud Maintainers.
- Pin all actions to their full commit SHA (not tags): `uses: actions/checkout@<SHA> # vX.Y.Z`.
- Never introduce actions from unverified third parties.

### Dependency Management
- Dependabot covers both GitHub Actions and Docker base-image updates for this
  repo (`.github/dependabot.yml`) — Renovate is not used here.
- Dependabot PRs do not auto-merge; review and merge them as part of regular
  maintenance. Merging one rebuilds and re-pushes the newest release.
- Base images in `Dockerfile.multiarch` must remain pinned as `tag@sha256:<digest>`;
  Dependabot updates the digest in place. A stale `golang` digest surfaces as Go
  `stdlib` CVEs failing the Trivy gate; the fix is bumping the digest, not adding
  `.trivyignore` entries.
- A HIGH/CRITICAL CVE in a Go module compiled into the binary blocks the build until
  upstream ships a fix. Only when upstream cannot fix it promptly does it go into
  `.trivyignore`, with the CVE, the reason and the upstream issue.

### Git Workflow
- **Rebase policy**: Always rebase; never create merge commits.
- **Signed commits**: All commits **must** be PGP/GPG signed (`git commit -S`).
- **DCO sign-off**: Every commit needs a `Signed-off-by` line (`git commit -s`).
- **Conventional Commits & Squash Merge**: PR titles must follow
  [Conventional Commits](https://www.conventionalcommits.org/); the PR title
  becomes the squash-merge commit message and is enforced by CI.

## Context for AI Agents

- This is a Docker-image build repo that compiles ocis-groupware from source — not the
  application codebase. Application changes belong upstream.
- The resolver is the only thing that decides what gets built: every verified upstream
  release missing from Docker Hub. Do not reintroduce a "newest version only"
  reduction for pending builds — it drops a release when two land in one polling
  window.
- Do not change the uid (65532), the data path (`/data`), the ports or the entrypoint
  path: upstream's Helm chart, docs and existing volumes depend on them.
- The image has no shell. Smoke tests and debugging must not rely on `/bin/sh`.
- The README is published verbatim as the Docker Hub image description — keep it
  accurate and self-contained.
- License is **Apache-2.0**, the OSPO's ecosystem-wide target; no relicensing needed.
