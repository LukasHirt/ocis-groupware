# ocis-groupware

Calendar and contacts for ownCloud Infinite Scale — CalDAV/CardDAV, Graph-shaped API, Vue UI

## Development

Requires Go 1.26.10 and [go-task](https://taskfile.dev).

```sh
task build
task test
GROUPWARE_OIDC_ISSUER=https://ocis.localhost GROUPWARE_DATA_DIR=$(mktemp -d) task run
```

Run `task --list-all` for the full list, or `task check` to run fmt/vet/lint/test
together (what CI runs).

Config is env vars and/or a YAML file passed via `--config groupware.yaml`; env wins
over file, file wins over defaults.

| Env var | YAML key | Notes |
|---|---|---|
| `GROUPWARE_HTTP_ADDR` | `http_addr` | Listen address, default `:8080` |
| `GROUPWARE_LOG_LEVEL` | `log_level` | `debug`, `info`, `warn` or `error`, default `info` |
| `GROUPWARE_OIDC_ISSUER` | `oidc_issuer` | **Required.** The oCIS public URL, which is also the OIDC issuer |
| `GROUPWARE_OIDC_AUDIENCE` | `oidc_audience` | Optional. If set, tokens must carry it in `aud`; oCIS sets that to the client ID of the requesting app, e.g. `web` |
| `GROUPWARE_OIDC_CA_FILE` | `oidc_ca_file` | Optional PEM bundle of extra CAs to trust when contacting the issuer |
| `GROUPWARE_DATA_DIR` | `data_dir` | **Required.** Directory that holds all service state. The image sets it to `/data` |
| `GROUPWARE_ADMIN_SOCKET` | `admin_socket` | Path of the admin API's unix socket, default `admin.sock` in the data directory. Leave it alone unless the data directory cannot hold a socket (see below) |

## Data directory

ocis-groupware keeps its state in one directory, `GROUPWARE_DATA_DIR`, and opens it with
an exclusive lock: a second process on the same directory fails to start. The directory
is created if missing and must be writable by the service. `/healthz` returns `503` when it is not.

The image runs as uid 65532 and declares `/data` as a volume owned by that user. The
compose stack mounts the named volume `groupware-data` there. When you bind-mount a host
directory instead, give uid 65532 write access to it.

Each user has a directory below `owners/` with a `principal.json`. Its `disabled` field,
when `true`, refuses every sign-in of that user, with app passwords and with bearer
tokens alike, until it is `false` again. Nothing else changes: calendars, contacts and
app passwords stay, so enabling the user restores every device without pairing it again.
A file without the field counts as enabled. Releases before the field was added do not
know it and ignore it, so a downgrade lets disabled users sign in again.

The service also creates `admin.sock` in the data directory, a unix socket that serves the
admin API: listing users, disabling and enabling them, and purging a disabled user's data.
The `admin` command (see [Administration](#administration)) uses it; so can
`curl --unix-socket /data/admin.sock http://admin/v1/principals`. There is no token. The
socket has mode 0600 and the data directory is private to the service's user, so only
that user and root can connect. The file is removed on
shutdown and a leftover from a crash is replaced on the next start. The service refuses to
start when it cannot create the socket, which happens on a filesystem without unix socket
support (some bind mounts of Docker Desktop) or when the path is longer than the platform
allows (104 bytes on macOS, 108 on Linux). Set `admin_socket` to a short path on a local
filesystem then.

## Authentication

Every request needs an `Authorization: Bearer <access token>` header, validated
against the keys the oCIS identity provider publishes. The exceptions are
`/healthz`, `/metrics`, `/.well-known/*` and `/web/*`. The token's `sub` claim
identifies the user.

CalDAV and CardDAV are served under `/dav`. There a request can also carry
`Authorization: Basic` with the username and an app password, which is how DAV
clients sign in. A request under `/dav` without valid credentials gets `401` with
`WWW-Authenticate: Basic realm="ocis-groupware"`. Each user reaches only their own
calendars and address books. A user's first request with a bearer token creates
their default address book and calendar.

## App passwords

DAV clients sign in with an app password instead of an OIDC token. Create one with a
bearer token for your user (`$TOKEN`):

```sh
curl -X POST https://ocis.localhost/groupware/v1/app-passwords \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name": "Phone"}'
```

The answer has status `201` and carries the `id`, the `name`, `created_at`, `expires_at`
and the `password`. **The password is shown only once**: it is stored as a hash, and no
request returns it again. An app password is valid for 365 days. A user can have at most
50, and a name has at most 100 bytes.

```sh
# List your app passwords (without their secrets)
curl -H "Authorization: Bearer $TOKEN" https://ocis.localhost/groupware/v1/app-passwords

# Revoke one; clients that use it are refused from then on
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://ocis.localhost/groupware/v1/app-passwords/<id>
```

The endpoint accepts bearer tokens only; an app password does not work there. A user
sees and revokes only their own app passwords, and revoking an id that does not exist or
belongs to someone else answers `404`.

To add an account in a CalDAV or CardDAV client, enter the server address (for example
`https://ocis.localhost`), your oCIS username (`preferred_username`) and the app password.
Clients that discover the account themselves find `/dav/` through `/.well-known/caldav` and
`/.well-known/carddav`. Use a separate app password for each device, so you can revoke one
without touching the others.

ocis-groupware contacts the issuer on the first authenticated request, and retries after
a failure, so it can start before oCIS is ready. While the issuer is
unreachable, authenticated requests get `503`.

## Administration

The same binary administers a running service through the admin socket. It reads the
configuration the way the service does, so in the container it needs no arguments
beyond the command:

```sh
docker exec <container> /ocis-groupware admin principals list
docker exec <container> /ocis-groupware admin principals disable alice
docker exec <container> /ocis-groupware admin principals enable alice
docker exec <container> /ocis-groupware admin principals purge alice
docker exec <container> /ocis-groupware admin principals purge alice --yes
```

Outside the container, pass `--config <file>` or set `GROUPWARE_DATA_DIR` (or
`GROUPWARE_ADMIN_SOCKET`); `--socket <path>` names the socket directly. The command
exits with 1 and says so when the service is not running: it has no way to open the
data directory itself.

`list` prints one user per line with the username, display name, state, creation time and
sub; `--json` prints the admin API's answer as it is. A user whose username was taken
over by a newer account shows `-` as username, and `--sub <sub>` reaches it where a
username is needed.

`disable` refuses every sign-in of the user, with app passwords and with bearer tokens
alike, and takes effect at once. Nothing is deleted, and `enable` lets the user back in
with every device still paired. Both are safe to repeat.

`purge` deletes a disabled user's calendars, contacts and app passwords, and the user's
directory with them. Without `--yes` it only shows what would go and exits with 1:

```
would purge alice (sub 4c510ada-c86b-4815-8820-42cdf82c3d51): 2 calendars, 1 address book, 3 app passwords
add --yes to delete this data; it cannot be undone
```

A user that is not disabled is refused, so the account cannot collect new data while
you decide. Purging does not keep the user out: if the account still exists in oCIS and
signs in again, it gets a fresh, empty profile with default collections. To keep someone
out, use `disable`. `purge --sub <sub> --yes` also removes a user directory that lost its
`principal.json`, say after an interrupted purge; such a directory does not appear in
`list`.

Every change is logged by the service at Info with the sub and username.

## Docker

```sh
task docker:build
task docker:run
```

## Local stack

`docker-compose.yml` runs oCIS and ocis-groupware behind one Traefik on the same
domain, which ocis-groupware requires. ocis-groupware's DAV, Graph, `/web/` and ops paths route
to ocis-groupware; everything else goes to oCIS.

```sh
task stack:up
```

Then open <https://ocis.localhost> (`admin` / `admin`). A `certs` service generates a
self-signed certificate on each start. Your browser will warn about it; ocis-groupware
trusts it through `GROUPWARE_OIDC_CA_FILE`.
Set `OCIS_DOMAIN`, `OCIS_VERSION` or `IDM_ADMIN_PASSWORD` in the environment
or a `.env` file to override the defaults. This stack is for development only.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Please also read the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Security

See [SECURITY.md](SECURITY.md) for how to report vulnerabilities.

## Support

See [SUPPORT.md](SUPPORT.md) for where to get help.

## License

[Apache License 2.0](LICENSE).
