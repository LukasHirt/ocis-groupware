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
is fixed. Deleting the file stops all sharing by that user. Shares end with what they
belong to: deleting a collection through the service drops its shares, a collection that
disappeared while the service was down is dropped at the next start, and purging a user
removes the user's shares. Shares that name a purged user as the grantee stay in the
owners' files and reach nothing.

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

A user whose data was imported before their first sign-in has no subject yet, only the
oCIS user id. At the first bearer sign-in with an unknown subject, the service asks oCIS
who the token belongs to. If an imported user without a subject has that id, the subject
is bound to it and the user finds the imported data. A user that already has a subject is
never taken over, and a disabled imported user is refused with `403`. While imported users
without a subject exist and oCIS cannot be reached, a sign-in with a new subject gets `503`
and is tried again on the next request, so that it does not create a second, empty user.

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

### Calendars

| Request | Answer |
|---|---|
| `GET /graph/v1.0/me/calendars` | The calendars the user sees, paged |
| `POST /graph/v1.0/me/calendars` | Creates a calendar from `name` and an optional `hexColor` (`#rrggbb`); `201` |
| `GET`, `PATCH /graph/v1.0/me/calendars/{id}` | One calendar; `PATCH` changes `name` and `hexColor` |
| `DELETE /graph/v1.0/me/calendars/{id}` | Deletes the calendar and its events; `204` |
| `GET`, `PATCH /graph/v1.0/me/calendar` | The default calendar, the oldest one the user owns |

A calendar carries `canEdit` (may change events), `canShare` (owner only) and
`owner.name`. A user who is not the owner gets `403` from `PATCH` and `DELETE`. An id
that is not one of the user's calendars answers `404`, whether it does not exist or
belongs to someone else. Calendars made here hold events, and the DAV URL of a new one
carries a generated name.

### Events

| Request | Answer |
|---|---|
| `GET /graph/v1.0/me/events`, `/me/calendar/events` | The events of the default calendar, the oldest one the user owns. A series is one item, its master |
| `GET /graph/v1.0/me/calendars/{id}/events` | The events of one calendar, own or shared |
| `GET /graph/v1.0/me/events/{id}`, `/me/calendars/{id}/events/{id}` | One event, or one occurrence of a series |
| `GET /graph/v1.0/me/calendarView?startDateTime=&endDateTime=` | The occurrences that overlap the window, series expanded, from every calendar the user sees; also below `/me/calendar` and `/me/calendars/{id}` |
| `POST /graph/v1.0/me/events`, `/me/calendar/events`, `/me/calendars/{id}/events` | Creates an event from the fields in the event mapping below; `start` and `end` are required; `201` |
| `PATCH /graph/v1.0/me/events/{id}` | Changes the fields in the body and leaves everything else of the stored event as it is; on an occurrence id it writes an override for that occurrence only |
| `DELETE /graph/v1.0/me/events/{id}` | Deletes the event with its series and overrides; on an occurrence id it excludes that date; `204` |

An event id is the calendar id, a dot and the URL-safe base64 form of the object's name in the
calendar. An occurrence id adds a dot and the occurrence's `RECURRENCE-ID` in UTC, such as
`...20261120T090000Z`. `@odata.etag` carries the object's ETag as a weak validator, and
`changeKey` the bare value. Events in the calendar view come with `type` `singleInstance`,
`occurrence` or `exception`, and occurrences have a `seriesMasterId`. The window may be up to five
years long; times without an offset are UTC. An exception date is left out, a moved occurrence
shows at its new time, and an occurrence that overlaps the window by any amount is included.

`If-Match` is honoured on `PATCH` and `DELETE` and answers `412` with the code `preconditionFailed`
when the event changed; `*` and the weak or bare form of the tag all work. Every write is also
checked against the ETag it read, so two writers cannot overwrite each other silently. A user
whose access to the calendar is read-only gets `403`. A `PATCH` on an occurrence cannot carry a
`recurrence`, and an occurrence that the series does not have, or that was excluded, answers
`404`. Changes show to DAV clients at the next sync like any other write, and no invitations or
updates are sent to attendees.

Times are shown in UTC, or in the zone of `Prefer: outlook.timezone="..."`, which the answer
repeats in `Preference-Applied`. `$top` and `$skip` page every list. `$filter`, `$select`,
`$orderby`, `$expand`, `$count`, `$search`, `delta` and `/instances` answer `501`.

### Event mapping

Events are stored as iCalendar and shown as Graph events. The mapping loses some detail,
and what it does not map is kept as it is.

| Graph | iCalendar |
|---|---|
| `subject`, `body.content`, `location.displayName` | `SUMMARY`, `DESCRIPTION`, `LOCATION`. Bodies are plain text; `html` is refused with `400` |
| `start`, `end`, `isAllDay` | `DTSTART`, `DTEND` (or `DURATION`). Times are shown in the zone of `Prefer: outlook.timezone="..."`, UTC by default. Floating times read as UTC |
| `showAs` | `TRANSP` and `STATUS`: `free` is transparent, `tentative` is `STATUS:TENTATIVE`, everything else is busy |
| `sensitivity` | `CLASS`. `personal` is stored as private |
| `categories`, `attendees` | `CATEGORIES`, `ATTENDEE` (`CN`, `ROLE`, `CUTYPE`, `PARTSTAT`). No invitations are sent |
| `isReminderOn`, `reminderMinutesBeforeStart` | the first display `VALARM` with a trigger before the start |
| `recurrence` | `RRULE`, for daily, weekly, monthly and yearly patterns, absolute and relative |

Zone names are IANA names or Windows names. A Windows name on a single event is stored as
the UTC time; on a recurring event it is kept as the zone of the rule. A rule the pattern
cannot express, such as one with `BYHOUR`, comes back as `recurrence: null` with
`"groupware.recurrenceEditable": false`, and a `PATCH` that sends `recurrence: null` leaves
it alone. A `PATCH` changes only the fields it sends and keeps every other property, alarm
and attendee parameter.

### Contact folders and contacts

| Request | Answer |
|---|---|
| `GET /graph/v1.0/me/contactFolders` | The address books the user sees, paged |
| `POST /graph/v1.0/me/contactFolders` | Creates an address book from `displayName`; `201` |
| `GET`, `PATCH /graph/v1.0/me/contactFolders/{id}` | One address book; `PATCH` changes `displayName` |
| `DELETE /graph/v1.0/me/contactFolders/{id}` | Deletes the address book and its contacts; `204` |
| `GET /graph/v1.0/me/contacts`, `/me/contactFolders/{id}/contacts` | The contacts of the default address book (the oldest one the user owns) or of one address book, sorted by display name, paged |
| `GET /graph/v1.0/me/contacts/{id}`, `/me/contactFolders/{id}/contacts/{id}` | One contact |
| `POST /graph/v1.0/me/contacts`, `/me/contactFolders/{id}/contacts` | Creates a contact from the fields in the contact mapping below; `201` |
| `PATCH /graph/v1.0/me/contacts/{id}` | Changes the fields in the body and leaves everything else of the stored card as it is |
| `DELETE /graph/v1.0/me/contacts/{id}` | Deletes the contact; `204` |

A contact folder has the id of its owner and its recipients, built like a calendar id. Besides
`id` and `displayName` it carries `groupware.isDefault`, `groupware.canEdit` (may change
contacts), `groupware.canShare` (owner only) and `groupware.owner.name`. A contact id is the folder
id, a dot and the URL-safe base64 form of the card's name in the address book. `@odata.etag`
carries the card's ETag as a weak validator, `parentFolderId` the folder id, and
`lastModifiedDateTime` the card's `REV`, or the time it was written.

Cards that stand for a group never show in a list, and asking for one by id answers `404`;
the facade does not write them. `If-Match` is honoured on `PATCH` and `DELETE` and answers `412`
with the code `preconditionFailed`. A `PATCH` that changes nothing does not write the card. A
user with read-only access to the address book, and anyone but the owner changing the folder
itself, gets `403`. Contacts made here show up for CardDAV clients at the next sync like any other
write. `$filter`, `$select`, `$orderby`, `$expand`, `$count`, `$search`, `delta` and child folders
answer `501`.

### Contact mapping

Contacts are stored as vCard 3.0 or 4.0 and shown as Graph contacts. The mapping loses
some detail, and what it does not map is kept as it is: a change touches only the
fields it sends, and a field that already has the value sent is not rewritten. Cards
made through the API are vCard 3.0.

| Graph | vCard |
|---|---|
| `displayName`, `surname`, `givenName`, `middleName`, `title`, `generation` | `FN`, and the five parts of `N`: family, given, additional, prefix, suffix. A display name that was built from the name parts follows them when a part changes; a new contact without one gets the name parts, the first e-mail address or the company |
| `nickName`, `jobTitle`, `profession`, `personalNotes` | `NICKNAME`, `TITLE`, `ROLE`, `NOTE` |
| `companyName`, `department` | the first two parts of `ORG` |
| `yomiGivenName`, `yomiSurname`, `yomiCompany` | `X-PHONETIC-FIRST-NAME`, `X-PHONETIC-LAST-NAME`, `X-PHONETIC-ORG` |
| `emailAddresses` | `EMAIL`. `name` is the address; a name sent in a request is ignored |
| `mobilePhone`, `businessPhones`, `homePhones` | `TEL`. `TYPE=CELL` is the mobile phone (the first one), `WORK` a business phone, and `HOME`, an unknown type or a custom label a home phone. Fax, pager and video numbers are not shown. A `tel:` value is shown without the prefix |
| `homeAddress`, `businessAddress`, `otherAddress` | `ADR` by `TYPE`; an address without a type is the other address. Only the first one of each place is shown. The post office box and the extended address stay as they are |
| `businessHomePage` | the first `URL` |
| `imAddresses` | `IMPP` and `X-SOCIALPROFILE`. New entries are written as `IMPP` |
| `categories` | `CATEGORIES` |
| `birthday` | `BDAY`, as `YYYY-MM-DDT00:00:00Z`. A birthday without a year has the year `0001`; it is stored as `--MMDD` in vCard 4 and with `X-APPLE-OMIT-YEAR` in vCard 3. A time of day is dropped |
| `spouseName`, `children`, `assistantName`, `manager` | `X-ABRELATEDNAMES` with the label Spouse, Child, Assistant or Manager, `RELATED` with `TYPE=spouse` or `child` and a name, and the `X-SPOUSE`, `X-MANAGER`, `X-ASSISTANT` forms of other clients. A `RELATED` that points at a card by URI is not a name |
| `groupware.groups` | read only: the group cards (`KIND:group`, or Apple's `X-ADDRESSBOOKSERVER-KIND`) that list the contact as a member, each with the `id` (the UID of the group card) and a `displayName` |

Labels that Apple clients write in `X-ABLabel` (`_$!<Work>!$_`, `_$!<Mobile>!$_`, `_$!<Home>!$_` and
custom text) decide the place of a phone number or address that has no `TYPE`. When a change
removes a property, its label goes with it. `fileAs`, `initials`, `officeLocation`, `photo`
and the other Graph fields are not mapped; a request that sends them leaves them out.


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

Every user also has an owner, the name the user's data is kept under. For someone who
signed in on their own it is the sub, and `list` leaves it out. A user can exist without a
sub, for example when their data was imported before their first sign-in. Then the owner
differs from the sub, `list` adds an OWNER column, and `--owner <owner>` reaches the user
wherever a username is needed.

`disable` refuses every sign-in of the user, with app passwords and with bearer tokens
alike, and takes effect at once. Nothing is deleted, and `enable` lets the user back in
with every device still paired. Both are safe to repeat.

`purge` deletes a disabled user's calendars, contacts and app passwords, the shares of
those collections, and the user's directory with them. Without `--yes` it only shows what would go and exits with 1:

```
would purge alice (sub 4c510ada-c86b-4815-8820-42cdf82c3d51): 2 calendars, 1 address book, 3 app passwords, 0 shares
add --yes to delete this data; it cannot be undone
```

A user that is not disabled is refused, so the account cannot collect new data while
you decide. Purging does not keep the user out: if the account still exists in oCIS and
signs in again, it gets a fresh, empty profile with default collections. To keep someone
out, use `disable`. `purge --owner <owner> --yes` also removes a user directory that lost
its `principal.json`, say after an interrupted purge; such a directory does not appear in
`list`. For a directory named after a sub, `--sub <sub>` does the same.

Every change is logged by the service at Info with the owner and username.

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

### Classic test instance

`test/classic` holds a classic ownCloud 10 server with seeded calendars, contacts,
shares, a public link, subscriptions and app tokens, together with committed
MariaDB and PostgreSQL dumps of it. It uses its own compose project and ports, so it
runs next to the stack above. The dumps are what tests read; the compose file and the
seeding script regenerate them. See [test/classic/README.md](test/classic/README.md).

`internal/migrate/classic` reads such a database (MariaDB, MySQL or PostgreSQL) and its
tests load the dumps into real databases. The schema it assumes is in
[docs/classic-database.md](docs/classic-database.md).

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
