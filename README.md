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
| `GROUPWARE_OCIS_URL` | `ocis_url` | Optional. Where the service calls the oCIS Graph API to look up the signed-in user's id and groups. Defaults to the issuer URL. Uses the same CA file |
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

The same file holds a snapshot of the user's oCIS identity: `ocis_id`, the `groups` the
user belongs to (each with `id` and `name`) and `identity_at`, the time the snapshot was
taken. A file without these fields has no snapshot yet and loads as before.

The snapshot is filled at sign-ins with a bearer token (the web app, and DAV clients that
use OIDC). The service calls `GET /graph/v1.0/me?$expand=memberOf` on `GROUPWARE_OCIS_URL`
with the user's own token, so it needs no credential for oCIS, and it asks at most once
every 5 minutes per user. If the call fails, the old snapshot stays and the failure is
logged; the sign-in still succeeds. Sign-ins with an app password carry no token and
do not refresh it. Group membership is therefore as old as the user's last sign-in with a
token, at most a few minutes behind while the user is active. The snapshot of a disabled
user is not updated.

An owner who shares a calendar or address book gets a `shares.json` next to it. It lists,
for each shared collection, the collection's store id and its members. A member is a user
or a group, named by the oCIS id, with a display name kept for listings and an access
level, `read` or `readwrite`. A collection has at most 200 members, and the owner cannot
be one of them. The service reads all of these files at start and keeps a lookup by
grantee in memory, so a share is stored once, with its owner. A `shares.json` the service
cannot parse is logged and skipped, which means its owner shares nothing until the file
is fixed. Deleting the file stops all sharing by that user.

A recipient sees a shared collection among their own as `shared-<collection id>`, with the
owner's name for it followed by the owner's display name in brackets. The access level of the
share decides what the recipient may do: `read` reads, `readwrite` also writes objects, and
only the owner changes the collection's description, time zone and components, or deletes it.
The recipient may change the display name, colour and order, because clients do that on
every calendar they add. Those changes are kept for that recipient alone, in
`shared_with_me.json` in the recipient's own directory, next to a flag that hides the
collection from listings. The file holds nothing else about the share. Access is decided
on each request from the recipient's oCIS id and group snapshot, so a revoked share, or a
group the user has left, stops working at once. The snapshot itself is as fresh as the
user's last sign-in with a token.

DAV clients find these collections in the recipient's own home, for example
`/dav/calendars/bob/shared-<id>/`, with the privileges of the share. A write the share does
not allow is answered `403` with `DAV:need-privileges`. This works for HTTP Basic and Bearer
requests alike. There is no way to create a share over DAV or through the service's API yet.

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

## Web application

The service serves its web application, a build of `web/`, under `/web/` without
authentication. The build output is embedded in the binary at compile time from
`web/dist`. That directory holds only a placeholder in git, so a binary built without
the web build answers `404` for every path under `/web/`, and the Go tests do not need
Node. The Docker image always contains the build.

`web/` is a Vue 3 and TypeScript project built with ownCloud's extension SDK. It is an
application for ownCloud Web, with Calendar and Contacts entries in the sidebar. oCIS
Web loads `/web/groupware.js` when the Web configuration lists the application under
`external_apps` (`deploy/ocis/web.yaml` is such a configuration, which the compose
stack mounts as `/etc/ocis/web.yaml`), and provides `vue`, `pinia`, `luxon`, `vue3-gettext`,
`@ownclouders/web-pkg` and `@ownclouders/web-client` to it at run time, so the bundle
contains none of them. `vue-router` is not provided; the application imports only
its types and routes through the composables of `web-pkg`. The ownCloud packages are
pinned to the Web version that the supported oCIS release ships (Web 12.5.0 in oCIS
8.2.x) and are updated by hand with it. [docs/web-integration.md](docs/web-integration.md)
records what Web provides to the application and which versions this was verified
against.

Development needs Node 24:

```sh
task web:install   # npm ci
task web:check     # eslint, vue-tsc, vitest and the build, what CI runs
task web:build     # writes web/dist; the next go build embeds it
```

In `web/`, `npm run dev` rebuilds on every change. A running local stack then serves
the new build only after the Go binary is rebuilt, since it is embedded.

Only `GET` and `HEAD` are answered, and only for plain files: no directory listings, no
index, nothing whose name starts with a dot. Each response carries its content type
and `X-Content-Type-Options: nosniff`. `/web/groupware.js`, the entry that oCIS Web
loads, is served with `Cache-Control: no-cache` because its name never changes. Every
other file in the build carries a content hash in its name and is served as immutable
for a year.

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

## Graph API

The calendar and contacts part of the Microsoft Graph API is served below
`/graph/v1.0/`. It takes the same OIDC bearer token as the other endpoints and nothing
else, so app passwords do not work here. Errors use Graph's body, `{"error": {"code",
"message", "innerError": {"request-id", "date"}}}`. Anything the service does not
implement answers `501` with the code `notImplemented`, so a client can tell an
unsupported request from a missing item.

Calendars and contact folders have the same id for their owner and for everyone they
are shared with: the URL-safe base64 form, without padding, of the collection id. List
answers carry `@odata.nextLink` when more items follow; `$top` (1 to 1000, default 100)
and `$skip` select the page.

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
to ocis-groupware; everything else goes to oCIS. [docs/routing.md](docs/routing.md) lists
those paths and has the same routing for Traefik's file provider, nginx and Caddy under
`deploy/`.

```sh
task stack:up
```

Then open <https://ocis.localhost> (`admin` / `admin`). The application menu lists
"Calendar and Contacts", which oCIS Web loads from ocis-groupware because the stack
mounts `deploy/ocis/web.yaml` into the oCIS configuration directory. A `certs` service
generates a self-signed certificate on each start. Your browser will warn about it;
ocis-groupware trusts it through `GROUPWARE_OIDC_CA_FILE`.
Set `OCIS_DOMAIN`, `OCIS_VERSION` or `IDM_ADMIN_PASSWORD` in the environment
or a `.env` file to override the defaults. This stack is for development only.

### Stack tests

`test/stack` holds end-to-end tests that need the running stack. They sign in as the
demo users through the oCIS identity provider, create app passwords and run CalDAV and
CardDAV client flows against `https://ocis.localhost/dav`: discovery through the
well-known paths, calendars and address books with their objects, queries, sync, and
the refusal of requests without credentials. They also check that oCIS announces the
web application in its Web configuration and serves its entry. The Go tests use the
[go-webdav](https://github.com/emersion/go-webdav) client. One test runs the Python
caldav client check that ships with groupkit, which also exercises occurrence
expansion.

`web/tests/e2e` holds a browser test, run with Playwright and a headless Chromium,
that signs in to oCIS Web as a demo user, opens the application from the application
switcher and moves between its pages. CI runs it in the stack job after the Go tests,
on both routes. By hand, with the stack up: `task web:install`, then
`npx playwright install chromium` in `web/`, then `task web:e2e`; `STACK_URL` picks
another stack address.

CI boots the stack with `OCIS_VERSION=8.2.2` and runs them on every pull request. The
tests are behind the `stack` build tag, so `task test` leaves them alone:

```sh
task stack:up
task stack:test
```

The tests take their settings from the environment. `STACK_URL` is the stack's address
(default `https://ocis.localhost`). `STACK_CA_FILE` is a PEM file with the stack's
certificate; without it the Go tests skip certificate verification, which the generated
development certificate needs. `STACK_PYTHON` names a Python interpreter with the
caldav client installed; the Python check runs only when it and `STACK_CA_FILE` are set:

```sh
docker compose cp traefik:/certs/cert.pem cert.pem
python3 -m venv .venv
.venv/bin/pip install --require-hashes \
  -r "$(go list -m -f '{{.Dir}}' github.com/LukasHirt/groupkit)/test/pyclient/requirements.txt"
STACK_CA_FILE=$PWD/cert.pem STACK_PYTHON=$PWD/.venv/bin/python task stack:test
```

The Go flows use `einstein` and remove what they create. The Python check uses `marie`
and expects her to have nothing but the default calendar, which is the case on a fresh
stack.

CI runs the tests a second time with the groupware paths routed through the proxy
inside oCIS instead of Traefik. `deploy/compose/ocis-proxy.yml` sets that up with the
routes in `deploy/ocis/proxy.yaml`, and `STACK_ROUTE=ocis-proxy` tells the tests which
route they are on. On that route the proxy keeps every REPORT request for itself, so
the CalDAV and CardDAV flows skip and `TestOCISProxyRoute` records what does and does
not get through. [docs/routing.md](docs/routing.md) explains why:

```sh
docker compose -f docker-compose.yml -f deploy/compose/ocis-proxy.yml up -d --build
STACK_ROUTE=ocis-proxy task stack:test
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Please also read the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Security

See [SECURITY.md](SECURITY.md) for how to report vulnerabilities.

## Support

See [SUPPORT.md](SUPPORT.md) for where to get help.

## License

[Apache License 2.0](LICENSE).
