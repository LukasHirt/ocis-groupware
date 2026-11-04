# Commit and PR Title Convention

Commits and PR titles follow [Conventional Commits](https://www.conventionalcommits.org).
PRs are squash-merged, so the PR title becomes the commit title on `main`, and the
release notes are generated from these titles with [git-cliff](https://git-cliff.org)
(see [cliff.toml](cliff.toml)).

```
type(scope): description
```

- `type`: `fix`, `feat`, `perf`, `revert`, `chore`, `test`, `build`, `ci`, `docs`, `style` or `refactor`
- `scope`: optional, the area of the change, for example `auth` or `server`
- `!` before the colon marks a breaking change: `feat(auth)!: require the audience claim`
- `description`: lowercase, imperative, no trailing period

## Release notes

Only `fix`, `feat`, `perf` and `revert` are listed, breaking changes additionally in their
own section. Each entry is the description, so it has to tell an admin what changed without
opening the PR:

| Instead of | Write |
|---|---|
| `fix: login` | `fix(auth): return 401 instead of 500 for an expired token` |
| `chore: fix metrics` | `fix(server): serve the metrics endpoint without authentication` |
| `feat: add tls` | `feat: add TLS support for the HTTP listener` |

## Preview the release notes

Install [git-cliff](https://git-cliff.org/docs/installation), then:

```bash
git cliff --unreleased
```
