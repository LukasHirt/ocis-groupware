# The migrate command

`ocis-groupware admin migrate classic` copies the calendars, address books, shares
and app tokens of a classic ownCloud installation into the data directory of the
service. It reads the classic database directly. Nothing is changed on the
classic side.

```
ocis-groupware admin [--config <file>] migrate classic --db-type <mysql|postgres>
    [--db-dsn <dsn>] [--table-prefix <prefix>] [--user <login>]...
    [--users <file>] [--groups <file>] [--dry-run] [--report-json <file>]
```

## Stop the service first

The command opens the data directory itself and writes to it, which the file
store allows for one process at a time. While the service runs, the command
refuses to start and says so. Stop the service, migrate, start it again. This
also means the command is not an admin socket call like the other `admin`
commands; it needs the same `GROUPWARE_DATA_DIR` or `--config` as the service,
and write access to the directory.

## What you need

- The classic database, MySQL, MariaDB or PostgreSQL, reachable from where the
  command runs. A read-only database user is enough. SQLite and Oracle
  installations are exported to MariaDB first. The schema the command assumes is
  in [classic-database.md](classic-database.md).
- A way to map classic ids to oCIS ids: `GROUPWARE_MIGRATE_OCIS_TOKEN` set to an
  administrator's access token (the command asks `ocis_url`, which defaults to
  the issuer), or CSV files given with `--users` and `--groups`. CSV entries win
  over the lookup. [classic-user-mapping.md](classic-user-mapping.md) has the
  details.
- `GROUPWARE_LEGACY_TOKEN_SECRET`, the classic instance secret, in the
  environment of the service that runs afterwards. Without it, imported app tokens
  are stored but never sign in. The command warns when the secret is not set where
  it runs.

The database address comes from `--db-dsn` or, to keep the password out of the
process list, from `GROUPWARE_MIGRATE_DB_DSN`. MySQL uses
`user:pass@tcp(host:3306)/database`, PostgreSQL uses
`postgres://user:pass@host:5432/database`. `--table-prefix` is needed only when
the classic `dbtableprefix` is not `oc_`.

## What is copied

For every enabled classic user that maps to an oCIS user:

- A principal with the classic login as username and the owner
  `migrated-<oCIS user id>`. It has no subject until the user signs in with a
  token for the first time, which binds it. Until then the user can sign in with
  an app password. No default calendar or address book is created for it.
- Their calendars and address books with URIs, names, descriptions, colours,
  order and time zone, and every event and contact byte for byte. The ETags
  differ, since the service derives them from the content, so clients do one full
  resync.
- Their app tokens, as app passwords with the same names. The token a device
  holds keeps working, and after its first use it becomes an ordinary app
  password. A token that was never used expires after 180 days.
  The imported token also needs the user to be confirmed by oCIS: it works for
  `GROUPWARE_IDENTITY_MAX_AGE` (30 days by default) after the import, and after
  that only once the user has opened oCIS Web or otherwise signed in with a
  token. Ask users to do that once after the migration.
- The shares of their collections, as grants to the oCIS user or group the
  recipient maps to. Read-only becomes read, read-write becomes read-write.

A user who has signed in to the service already is found by oCIS id, and the data
is added to what they have.

## What is not copied

The report lists these:

- Public links.
- Calendar subscriptions.
- The birthday calendar and the system address book, which classic generates.
- Calendars and address books owned by a group or the system.
- Objects the service does not hold: tasks in a calendar, and objects that are not
  valid iCalendar or vCard. They are counted and named in the report.
- Users and groups without an oCIS counterpart, and disabled accounts. Their data
  is skipped and they are listed.

Account passwords are not migrated either. Devices that signed in with the
account password need an app password. The trashbin, deleted calendars and the
scheduling inbox and outbox are not read.

## Running it

Start with a dry run. It reads everything, writes nothing and prints what a real
run would do:

```
GROUPWARE_MIGRATE_OCIS_TOKEN=... GROUPWARE_MIGRATE_DB_DSN=... \
  ocis-groupware admin migrate classic --db-type mysql --dry-run
```

Then run it without `--dry-run`. `--user alice` limits a run to one user, and can
be repeated. The recipients of that user's shares are still mapped, so the shares
are written even when the recipient is migrated in a later run. Shares from other
users to alice are written when their owner is migrated.

The report is printed as text. `--report-json <file>` also writes it as JSON. The
exit code is 1 when the report lists problems, such as an object that could not be
written, and the text says which.

Copying goes at the speed of the disk, because the file store flushes each write.
Five thousand events take a minute or two on a laptop.

## Running it again

A rerun adds only what is new. Principals are matched by oCIS id, collections by
name, objects by name and content, app tokens by name, and shares by recipient.
Nothing is overwritten or deleted: an object that differs from the classic copy,
because someone edited it in the new service, is kept and counted as different in
the report. A token that was used since the first run no longer has its classic
digest, which is why tokens are matched by name.

Two objects in one calendar can share a UID in a classic database. Both are
copied; the second one is stored without an entry in the UID index, and the report
names it.
