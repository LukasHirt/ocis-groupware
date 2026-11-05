# Reading a classic database

The package `internal/migrate/classic` reads the data of a classic ownCloud 10
installation from its database. It only issues `SELECT` statements. This page
lists what it assumes about the schema, so an administrator can check an
installation against it before a migration.

## Databases

| Database | Driver | Connection string |
|---|---|---|
| MySQL, MariaDB | `go-sql-driver/mysql` | `user:pass@tcp(host:3306)/dbname` |
| PostgreSQL | `jackc/pgx` | `postgres://user:pass@host:5432/dbname` |

SQLite and Oracle are not supported. An installation on either one has to be
exported to MariaDB first.

The tables are expected to carry the prefix `oc_`. An installation with another
`dbtableprefix` sets it explicitly. The reader accepts only letters, digits and
underscores in the prefix.

## Tables and columns

The reader was written against the schema of the 10.15 release and is tested
against dumps of it for both database kinds (`test/classic`). Other 10.x
releases may have other column sets; the reader selects the columns it needs by
name and does not depend on the others.

| Data | Table | Used columns |
|---|---|---|
| Users | `oc_accounts` | `user_id`, `display_name`, `email`, `state`, `backend` |
| Groups | `oc_groups`, `oc_group_user` | `gid`, `uid` |
| Calendars | `oc_calendars` | `id`, `principaluri`, `uri`, `displayname`, `description`, `calendarcolor`, `timezone`, `calendarorder`, `components`, `transparent`, `synctoken` |
| Events and tasks | `oc_calendarobjects` | `calendarid`, `uri`, `calendardata`, `etag`, `lastmodified`, `componenttype`, `uid`, `classification`, `size` |
| Subscriptions | `oc_calendarsubscriptions` | `principaluri`, `uri`, `source`, `displayname`, `calendarcolor`, `calendarorder`, `refreshrate` |
| Address books | `oc_addressbooks` | `id`, `principaluri`, `uri`, `displayname`, `description`, `synctoken` |
| Cards | `oc_cards` | `addressbookid`, `uri`, `carddata`, `etag`, `lastmodified`, `size` |
| Shares | `oc_dav_shares` | `principaluri`, `type`, `access`, `resourceid`, `publicuri` |
| App passwords | `oc_authtoken` | `uid`, `login_name`, `name`, `token`, `type`, `last_activity` |

## Assumptions

- **Principals.** Owners and share recipients are stored as `principals/users/<uid>`,
  `principals/groups/<gid>` or `principals/system/system`. Any other form is an
  error that names the row.
- **System principal.** The system address book, which lists every user, and the
  birthday calendar that classic generates from contact birthdays can be owned by
  the system principal or by a user. The reader returns them like any other
  calendar or address book and leaves skipping them to the caller.
- **Share access.** In `oc_dav_shares.access`, 2 means read-write and 3 means
  read-only. Any other value is read as read-only, so an unknown value never
  widens access.
- **Public links.** A row with a non-empty `publicuri` is a public link. Its
  principal is the owner and it has no recipient.
- **Shared resources.** `resourceid` is the row id of a calendar or an address
  book, depending on `type` (`calendar` or `addressbook`).
- **Accounts.** The user list comes from `oc_accounts`, which also holds users of
  other backends. `state` 0 (not signed in yet) and 1 count as enabled, 2
  (disabled) and 3 (deleted) as not enabled. Group membership comes from
  `oc_group_user`; groups provided only by an external directory are not in the
  database and are not read.
- **App passwords.** Only rows of type 1 are app passwords; browser sessions are
  stored in the same table with type 0 and are ignored. The `token` column holds
  the hex SHA-512 of the token followed by the instance secret. The secret is in
  `config.php` as `secret`, not in the database.
- **Objects.** `calendardata` and `carddata` are returned as stored, byte for
  byte. Calendars and address books of any size are streamed row by row.
- **Text.** The reader expects the database to return UTF-8 text.

## Tests

`go test ./internal/migrate/classic` loads the committed dumps into the
databases named by `CLASSIC_TEST_MYSQL_DSN` and `CLASSIC_TEST_POSTGRES_URL` and
skips the database tests when they are unset. `CLASSIC_REQUIRE_DB=1` turns a
missing database into a failure. CI starts MariaDB and PostgreSQL as service
containers and sets all three.

To run them locally:

```
docker run -d --name classic-mariadb -p 127.0.0.1:23306:3306 \
  -e MARIADB_ROOT_PASSWORD=root -e MARIADB_DATABASE=classic mariadb:10.11
docker run -d --name classic-postgres -p 127.0.0.1:25432:5432 \
  -e POSTGRES_PASSWORD=pg -e POSTGRES_DB=classic postgres:16

CLASSIC_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:23306)/classic' \
CLASSIC_TEST_POSTGRES_URL='postgres://postgres:pg@127.0.0.1:25432/classic?sslmode=disable' \
go test ./internal/migrate/classic
```

The loader drops the classic tables before it loads, so point it at a scratch
database.
