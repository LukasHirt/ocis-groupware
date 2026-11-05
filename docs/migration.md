# Moving from a classic ownCloud installation

This guide is for an administrator who runs ownCloud 10 with its Calendar and
Contacts apps and wants to move that data to oCIS with ocis-groupware without
asking every user to set up their devices again. It covers the preparation, the
move, what to check afterwards, what does not come along and how to go back.

The command is described in [classic-migrate.md](classic-migrate.md), the mapping
of users in [classic-user-mapping.md](classic-user-mapping.md) and the routing in
[routing.md](routing.md). This page puts them in order.

## What you get

Calendars and address books arrive with their names, descriptions, colours, order
and time zones, and every event and contact arrives byte for byte. Shares of those
collections arrive as shares to the oCIS users and groups the recipients map to.
Each app token a user created for a device is imported, so the device keeps its
login. With the classic paths switched on, the service answers the addresses the
devices already hold (`/remote.php/dav/...`), so nothing has to be typed again.

Each device does one full resync after the move. The new service gives every
object a different ETag than the classic server did, and the sync tokens of the
classic server mean nothing to it. The client notices and fetches everything
again. On a large calendar that takes a while, and it happens once.

## Before you start

### What you need

- oCIS 8, with an ingress in front of it that sends the groupware paths to the
  service. [routing.md](routing.md) lists them.
- The same logins in oCIS. The classic paths contain the user's name in every
  address, so a user who is `alice` on the classic server has to be `alice` in oCIS.
  That is normally the case when both servers read one directory. A user whose login
  differs can be migrated with a CSV file, but their devices have to be set up again,
  because the addresses they hold contain the old name.
- Read access to the classic database. MySQL, MariaDB and PostgreSQL work, and a
  read-only database user is enough. SQLite and Oracle are not supported. Export
  such a database to MariaDB first.
- An administrator's access token for oCIS, which the command uses to find the oCIS
  id of every classic user and group, or CSV files that list the pairs.
- The classic instance secret, the `secret` value in the classic `config.php`. The
  service needs it to recognise the imported app tokens. Treat it like a password:
  with the database and the secret, anyone can check guesses against the token
  digests.
- Disk speed. The service flushes every write, so copying goes at the speed of the
  disk. Five thousand events take a minute or two on a laptop.

### What to decide

- Who is migrated. Every user the lookup can map is migrated. Disabled classic
  accounts and users and groups without an oCIS counterpart are skipped and listed.
  `--user <login>` limits a run to some users, and the command can be run again for
  the rest.
- When. The migration reads a snapshot, so anything a user changes on the classic
  server after the final run is not in the new service. Plan a window in which the
  classic server takes no writes, for example with `occ maintenance:mode --on`. Dry
  runs and test runs can happen at any time.
- How to route. The classic addresses need an ingress. The proxy inside oCIS cannot
  carry them, see [routing.md](routing.md).

### Preparation, in order

1. Back up the classic database and its `config.php`. The command only reads, but a
   backup costs little.
2. Install ocis-groupware next to oCIS and check that sign-in, the web app and
   `/dav` work for a test user. Do this with a separate data directory, or be ready
   to remove the data directory before the real run, because test users stay in it.
3. Switch on the classic paths. Set `GROUPWARE_COMPAT_PATHS=true` and
   `GROUPWARE_LEGACY_TOKEN_SECRET` to the classic secret, and add the three
   `/remote.php/dav/` routes to the ingress (snippets below). A request without
   credentials to `/remote.php/dav/calendars/alice/` must now get `401` with
   `WWW-Authenticate: Basic realm="ocis-groupware"`, and a request to
   `/remote.php/dav/files/alice/` must still be answered by oCIS.
4. Run the command as a dry run, with the service stopped. It reads everything,
   writes nothing and prints what a real run would do.
5. Read the report. It names every user without an oCIS counterpart, every object it
   cannot take over and everything that stays behind. Fix the mapping, or accept
   what is left, before the real run.
6. Tell your users when the move happens, that the first sync afterwards can take a
   while, and that they should open oCIS Web once afterwards.

## The move

None of these steps needs the classic server to go away.

1. Put the classic server into maintenance mode, so that clients get an error
   instead of sending changes that would not be migrated.
2. Stop the ocis-groupware service. The command opens the data directory itself and
   refuses to start while the service holds it.
3. Run the command. With the container image it looks like this:

   ```sh
   docker compose stop ocis-groupware
   docker compose run --rm --no-deps \
     -e GROUPWARE_MIGRATE_DB_DSN='user:pass@tcp(db:3306)/owncloud' \
     -e GROUPWARE_MIGRATE_OCIS_TOKEN="$ADMIN_TOKEN" \
     -v "$PWD/report:/report" \
     ocis-groupware admin migrate classic --db-type mysql \
       --report-json /report/migration.json
   ```

   `--table-prefix` is needed when the classic `dbtableprefix` is not `oc_`. For
   PostgreSQL use `--db-type postgres` and an address like
   `postgres://user:pass@host:5432/database`. The report directory has to be
   writable for the container's user, which is 65532. A run that finishes with
   problems in its report exits with status 1, and the text says which.
4. Read the report and compare the numbers with what you expect: users, calendars,
   address books, objects, app tokens and shares.
5. Start the service with `GROUPWARE_LEGACY_TOKEN_SECRET` and
   `GROUPWARE_COMPAT_PATHS=true` set.
6. Make the classic host name reach oCIS and the service, if it did not already.
   Devices that keep talking to the old host name now reach the new service.
7. Check one device. Add a classic address, user name and app token to a test client
   and look at the calendars.

The command can be run again. It adds what is new, compares names and contents and
overwrites nothing. When you migrate in stages, run it again for the remaining
users. The shares a user made are written when that user is migrated, and a
recipient who is migrated later still sees them.

## What moves and what does not

These move:

| What | Notes |
|---|---|
| Calendars and address books of every mapped user | With URIs, names, descriptions, colours, order and time zones |
| Events and contacts | Byte for byte. The ETags change |
| App tokens | Same name, same secret. They sign in over Basic with the classic user name |
| Shares of calendars and address books | To the mapped user or group. Read-only becomes `read`, read-write becomes `readwrite` |

These do not:

| What | What to do |
|---|---|
| Public links to a calendar | Create them again if they are still wanted. The report lists them |
| Calendar subscriptions (WebCal) | Subscribe again in the client. The report lists the source addresses |
| Tasks in a calendar | The service stores events only. Export the tasks from the classic server before you retire it. The report counts them |
| The birthday calendar and the system address book | Classic generates both from other data. They are not copied |
| Calendars and address books owned by a group or the system | Not read |
| Objects that are not valid iCalendar or vCard | Counted and named in the report |
| Trashbin, deleted calendars, the scheduling inbox and outbox | Not read |
| Disabled accounts, and users and groups without an oCIS counterpart | Skipped and listed |
| Account passwords | Devices that signed in with the account password need an app password |

A calendar shared with a classic group is shared with the oCIS group that the group
maps to. The service keeps a snapshot of group members that is taken when a user
signs in with a token, so a member who has not signed in since joining the group is
not yet seen as one.

## What each device needs

The aim is that devices need nothing. Whether that holds depends on how they signed
in.

| The device was set up with | It needs |
|---|---|
| The classic address and an app token | Nothing. It signs in with the old token, which is stored as a new hash at its first use, and does one full resync |
| The classic address and the account password | A new app password, since account passwords are not migrated. The user creates one after signing in to oCIS (see App passwords in the README) and enters it in the client |
| A classic login that differs from the oCIS login | To be added again with the new address and user name |
| A new device | The server address `https://<host>` and an app password. Discovery finds `/dav/` |

Some details apply to all of them:

- The full resync is expected and needs no action. A client asks for the changes
  since a sync token the new service does not know, gets an error and starts over. A
  client that does not recover can be told to refresh the account, or the account can
  be removed and added again.
- An imported token that is never used expires 180 days after its import, or after
  its last use on the classic server. Set `GROUPWARE_LEGACY_TOKEN_TTL` to change
  that period. After its first successful use the token is an ordinary app password
  and is valid for 365 days.
- A migrated user has no oCIS subject until they sign in once with a token, in the
  web app or with a client that uses OIDC. At that sign-in the service binds the
  migrated data to them. Sign-ins with an app token work before and after it. The
  user's name in the service is the oCIS login from then on.
- The service only accepts an app password while oCIS has confirmed the user within
  `GROUPWARE_IDENTITY_MAX_AGE`, 30 days by default. The import counts as a
  confirmation. A user who never opens oCIS afterwards finds every device refused
  with `401` once that time has passed, and one visit to oCIS Web fixes it. Ask
  users to open oCIS Web once soon after the move, which also binds their data.
- Tasks and subscriptions are gone from calendar apps that used them, as listed
  above.

## After the move

- Keep the report with the date and the command line.
- Watch the logs for rejected sign-ins in the first days. A client with a wrong
  secret shows up as a run of `401` answers, or as `429` when it retries quickly,
  because checks of imported tokens are limited.
- Users find their calendars and address books in the web app after their first
  sign-in. Collections shared with them appear in DAV clients as `shared-<id>`,
  named like the original.
- Keep the classic server until you are sure. The migration does not change it.
- Back up the data directory once the move has settled.

## Rolling back

The migration changes nothing on the classic side, so going back is a matter of
routing and of what users did in the meantime.

If nobody has written to the new service yet:

1. Point the host name back at the classic server and take it out of maintenance
   mode.
2. Stop the service and move its data directory away. Run the migration into an
   empty directory when you try again.

If users have written to the new service, the classic server does not have those
changes, and there is no migration back. Either accept the loss, or first download
the calendars as `.ics` and the address books as `.vcf` files (see Import and
export in the README) and import them into the classic server by hand. Devices do a
full resync in both directions, since the sync tokens of the two servers do not
match.

## Ingress snippets for the classic URLs

Besides the groupware paths in [routing.md](routing.md), the ingress sends these
three prefixes to the service and nothing else below `/remote.php/`:

| Path | What it is |
|---|---|
| `/remote.php/dav/principals/` | Principals |
| `/remote.php/dav/addressbooks/` | CardDAV |
| `/remote.php/dav/calendars/` | CalDAV |

`/remote.php/dav/files/` and the rest stay with oCIS. The service needs
`GROUPWARE_COMPAT_PATHS=true`.

Traefik, as a router next to the existing one:

```yaml
    ocis-groupware-compat:
      rule: >-
        Host(`ocis.example.com`) && (
        PathPrefix(`/remote.php/dav/principals/`) ||
        PathPrefix(`/remote.php/dav/addressbooks/`) ||
        PathPrefix(`/remote.php/dav/calendars/`))
      entryPoints: [websecure]
      service: ocis-groupware
      priority: 100
```

nginx, inside the oCIS server block:

```nginx
    location ^~ /remote.php/dav/principals/ {
        proxy_pass http://ocis_groupware;
    }
    location ^~ /remote.php/dav/addressbooks/ {
        proxy_pass http://ocis_groupware;
    }
    location ^~ /remote.php/dav/calendars/ {
        proxy_pass http://ocis_groupware;
    }
```

Caddy, next to the existing matcher:

```caddy
    @groupware_compat path /remote.php/dav/principals/* /remote.php/dav/addressbooks/* /remote.php/dav/calendars/*

    handle @groupware_compat {
        reverse_proxy ocis-groupware:8080
    }
```

`deploy/compose/compat-paths.yml` does the same for the development stack. Only an
ingress can carry these paths. The proxy inside oCIS answers every `REPORT` below
`/remote.php/dav` itself, so no query or sync request would reach the service. The
ingress has to pass the WebDAV methods through, which are PROPFIND, PROPPATCH,
REPORT, MKCOL, MKCALENDAR, PUT, DELETE, MOVE and COPY. If a firewall filters methods,
allow them for these paths. After a change to the ingress, wait a moment before
testing: Traefik needs a second to notice a replaced container.

## What is tested

The stack test `TestMigrateClassicInstance` runs the move against oCIS 8.2.2 in CI,
with the fixture from `test/classic`: the dump is loaded into MariaDB, the migration
runs against the stopped service, and a client with the classic address and an app
token keeps syncing. [classic-migrate.md](classic-migrate.md) lists what it checks
and what it does not reach. Real client programs, a PostgreSQL source in the stack
test and a classic server with its own directory service are not part of it.
