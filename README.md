# oCIS Groupware Docker Image

[![Docker Pulls](https://img.shields.io/docker/pulls/owncloud/ocis-groupware.svg)](https://hub.docker.com/r/owncloud/ocis-groupware)
[![License: Apache-2.0](https://img.shields.io/github/license/owncloud-docker/ocis-groupware)](https://github.com/owncloud-docker/ocis-groupware/blob/main/LICENSE)
[![ownCloud OSPO](https://img.shields.io/badge/OSPO-ownCloud-blue)](https://kiteworks.com/opensource)

Docker image for [oCIS Groupware](https://github.com/owncloud/ocis-groupware) — calendar and
contacts for [ownCloud Infinite Scale (oCIS)](https://github.com/owncloud/ocis): CalDAV and
CardDAV, a Graph-shaped API and a web UI for ownCloud Web. This repo builds and publishes the
image; the application source lives in
[owncloud/ocis-groupware](https://github.com/owncloud/ocis-groupware).

> **Build & maintenance:** see [how these images are built, scanned, updated and published](https://github.com/owncloud-docker/.github/blob/master/docs/IMAGE-LIFECYCLE.md).

## Quick Start

```bash
docker run -d --name groupware \
  -v groupware-data:/data \
  -e GROUPWARE_OIDC_ISSUER=https://ocis.example.com \
  -p 8080:8080 \
  owncloud/ocis-groupware:latest
```

`GROUPWARE_OIDC_ISSUER` is the public URL of your oCIS instance and the only required
setting. The service must be served on the same domain as oCIS, behind the same reverse
proxy. See the upstream
[README](https://github.com/owncloud/ocis-groupware#readme),
[administration guide](https://github.com/owncloud/ocis-groupware/blob/main/docs/administration.md)
and [routing guide](https://github.com/owncloud/ocis-groupware/blob/main/docs/routing.md)
for every `GROUPWARE_*` setting and the proxy routes. A Helm chart lives in the upstream
repository under `deploy/helm/ocis-groupware`.

## Supported Tags

| Tag | Meaning |
|-----|---------|
| `latest` | The highest released version |
| `X.Y.Z` | A specific release (e.g. `0.1.0`), rebuilt in place while it is the newest one |
| `X.Y.Z-YYYYMMDD` | The build of a release on a given day (e.g. `0.1.0-20261111`); never touched again after that day |

> **Note:** oCIS Groupware is pre-1.0. Only plain `vX.Y.Z` upstream release tags are
> built; there are no floating `X.Y` or `X` tags. For production, pin an `X.Y.Z` or
> `X.Y.Z-YYYYMMDD` tag instead of `latest`. A second rebuild on the same day replaces
> that day's `X.Y.Z-YYYYMMDD` tag; pin by digest if you need the exact bytes.

```bash
docker pull owncloud/ocis-groupware:latest
```

## How New Releases Get Built

- **New releases:** CI checks the upstream repository for new release tags every 6 hours
  and builds every release that is not on Docker Hub yet. Maintainers can start the same
  check by hand (`workflow_dispatch`) instead of waiting.
- **Signed tags only:** a release is built only from an annotated upstream tag whose
  signature GitHub verifies. Other tags are skipped with a warning.
- **Security rebuilds:** the newest release is rebuilt and re-pushed whenever a change is
  merged into this repository, including base-image and Go toolchain updates, and once a
  week.
- **Scanning:** every build is scanned with Trivy and smoke-tested before it is
  published. An image with a fixable HIGH or CRITICAL vulnerability is never pushed.

## Volumes

| Path | Purpose |
|------|---------|
| `/data` | All service state (`GROUPWARE_DATA_DIR`): calendars, contacts, app passwords and the admin socket |

The service locks the directory, so only one container can use a volume at a time.

## Ports

| Port | Protocol | Purpose |
|------|----------|---------|
| `8080` | TCP | Public HTTP: DAV, Graph API, web app (`GROUPWARE_HTTP_ADDR`) |
| `9180` | TCP | Unauthenticated `/metrics` and `/healthz` (`GROUPWARE_DEBUG_ADDR`) — keep it off the ingress and any untrusted network |

## User and Filesystem

The image is based on
[distroless `static`](https://github.com/GoogleContainerTools/distroless) and runs as
uid/gid `65532`. `/data` is owned by that user. When you bind-mount a host directory
instead of a named volume, give uid `65532` write access to it.

There is no shell or package manager in the image. To debug a running container, use
`kubectl debug` or an ephemeral container. The binary's own admin commands work directly:

```bash
docker exec groupware /ocis-groupware admin principals list
```

## Build Arguments

| ARG | Default | Purpose |
|-----|---------|---------|
| `GIT_REF` | `main` | `ocis-groupware` git ref (tag or branch) to clone and build, e.g. `v0.1.0` |
| `GIT_SHA` | `""` | Optional exact commit to check out after cloning `GIT_REF`. CI passes the commit the verified tag points to |
| `VERSION` | `""` | Version string embedded in the OCI labels, e.g. `0.1.0` |
| `REVISION` | `""` | Git commit embedded in the OCI labels |
| `TARGETARCH` | set by buildx | Target architecture (`amd64`, `arm64`) |

## Building

The image is built entirely from source via a four-stage `Dockerfile.multiarch`:

**`source`** — clones `ocis-groupware` at `${GIT_REF}`, pinned to `${GIT_SHA}` when set.

**`web`** — builds the Vue web application (`npm ci && npm run build`).

**`build`** — compiles the Go binary with `CGO_ENABLED=0` for the target architecture,
embedding the web application.

**Runtime** — distroless `static` (nonroot) with only the binary and the `/data`
directory.

```bash
docker buildx build -f Dockerfile.multiarch \
  --build-arg GIT_REF=v0.1.0 \
  --build-arg VERSION=0.1.0 \
  -t ocis-groupware:test .
```

## CI

The GitHub Actions workflow (`.github/workflows/main.yml`) runs on pull requests,
pushes to `main`, a schedule and by hand:

1. **Test** — lints the scripts and tests the release resolution logic.
2. **Resolve** — works out which releases to build (see
  [How New Releases Get Built](#how-new-releases-get-built)). A pull request always builds
  the newest release (or upstream `main` while there is none, or it does not verify), without publishing it.
3. **Build** — multi-arch image (`linux/amd64`, `linux/arm64`), built natively on each
  architecture.
4. **Trivy scan** — fails on HIGH/CRITICAL CVEs that have a fix; accepted exceptions are
  listed with a justification in `.trivyignore`.
5. **Smoke test** — starts the container and polls `http://localhost:9180/healthz`.
6. **Publish** — pushes to Docker Hub and syncs this README as the image description
  (not on pull requests).

## Community & Support

- [ownCloud Website](https://owncloud.com)
- [Community Discussions](https://github.com/orgs/owncloud/discussions)
- [Matrix Chat](https://app.element.io/#/room/#owncloud:matrix.org)
- [Documentation](https://doc.owncloud.com)
- [Enterprise Support](https://owncloud.com/contact-us/)
- [OSPO Home](https://kiteworks.com/opensource)

See [SUPPORT.md](SUPPORT.md) for the full list of support channels.

## Contributing

We welcome contributions! Please read the [Contributing Guidelines](CONTRIBUTING.md)
and our [Code of Conduct](CODE_OF_CONDUCT.md) before getting started.

- **Rebase Early, Rebase Often!** We use a rebase workflow — rebase on the target
  branch before submitting a PR.
- **Signed commits**: All commits **must** be PGP/GPG signed and carry a DCO
  `Signed-off-by` line (`git commit -S -s`).
- **Conventional Commits**: PR titles must follow the
  [Conventional Commits](https://www.conventionalcommits.org/) format — enforced
  by CI.
- **GitHub Actions Policy**: Workflows may only use actions owned by `owncloud`,
  created by GitHub (`actions/*`), or verified in the GitHub Marketplace, pinned
  to a full commit SHA.

Note: bug reports and feature requests for oCIS Groupware itself belong upstream in
[`owncloud/ocis-groupware`](https://github.com/owncloud/ocis-groupware) — this repository
tracks only the Docker packaging.

## Security

**Do not open a public GitHub issue for security vulnerabilities.**

Report vulnerabilities at **<https://security.owncloud.com>** — see [SECURITY.md](SECURITY.md).

## About the ownCloud OSPO

The [Kiteworks Open Source Program Office](https://kiteworks.com/opensource), operating under
the [ownCloud](https://owncloud.com) brand, launched on May 5, 2026, to steward the open source
ecosystem around ownCloud's products. The OSPO ensures transparent governance, license compliance,
community health, and sustainable collaboration between the open source community and
[Kiteworks](https://www.kiteworks.com), which acquired ownCloud in 2023.

- **OSPO Home**: <https://kiteworks.com/opensource>
- **GitHub**: <https://github.com/owncloud>
- **ownCloud**: <https://owncloud.com>

For questions about the OSPO or licensing, contact ospo@kiteworks.com.

This repository is licensed under the **Apache License 2.0**, which is the license
the OSPO is adopting across the ecosystem. No relicensing is required.

## License

Apache-2.0 — see [LICENSE](LICENSE).
