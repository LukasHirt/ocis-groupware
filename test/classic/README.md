# Classic test instance

This directory holds a classic ownCloud 10 server with a seeded calendar and
contacts, and the database dumps taken from it. Tests that read a classic
database use the dumps, so they run without Docker. The compose file and the
seeding script exist to regenerate the dumps.

## What is seeded

| Area | Content |
|---|---|
| Users | `alice`, `bob`, `carol` (password is the name plus `-pass`), groups `team` (bob, carol) and `staff` (alice, bob) |
| Calendars | `alice/work` with 5,000 events, `alice/personal` (time zone, a moved occurrence, an all-day range, a private tentative event), `alice/archive` (non-ASCII name), `bob/projects` |
| Event mix in `work` | plain, all-day, weekly recurrence with an excluded date, reminders, an external attendee, escaped text, non-ASCII text, categories and locations |
| Contacts | 120 cards in `alice/contacts`, 15 in `alice/friends`, 10 in `bob/contacts`; some with birthdays, organisations and notes |
| Shares | `alice/work` to bob (read-write) and to group `team` (read-only); `alice/personal` to carol; `bob/projects` to alice (read-write); `alice/friends` to bob (read-write) and `team`; `bob/contacts` to alice |
| Public link | one on `alice/work` |
| Subscriptions | a `Holidays` subscription for alice and for bob (the source URL does not resolve) |
| App tokens | `alice` "Phone One" and "DAVx5 on Pixel", `bob` "Thunderbird" |

The instance secret is fixed to a known value before the tokens are made. The
classic server stores an app token as `sha512(token . secret)`, so the
description next to each dump lists the plaintext, the secret and the expected
digest. The tokens exist only in this throwaway fixture.

The server also fills a `contact_birthdays` calendar from the birthdays on the
cards. Those objects are part of the dump and counted in the description.

## Files

- `compose.yml`: the server (`owncloud/server`, 10.15 by default, override with
  `CLASSIC_VERSION`) with MariaDB 10.11, and under the `postgres` profile a
  second server with PostgreSQL 16. The project is `classic-fixture` and the
  ports are 18080 (MariaDB) and 18081 (PostgreSQL), so it can run next to the
  main compose stack. The server image installs the calendar and contacts apps
  from the marketplace at first start, so the host needs internet access.
- `seed.sh`: creates users, groups, calendars, events, contacts, shares, the
  public link, subscriptions and app tokens through DAV, occ and the personal
  security page, then writes the dump and its description.
- `dumps/mariadb.sql`, `dumps/postgres.sql`: schema and rows of the tables the
  migration reads (users, accounts, groups, calendars and their objects,
  subscriptions, shares, address books and cards, properties, change logs and
  auth tokens). One `INSERT` per row. Temporary session tokens are removed
  before the dump.
- `dumps/mariadb.json`, `dumps/postgres.json`: the secret, the users, the app
  tokens with their digests and the expected row and event counts.
- `fixture_test.go`: checks that each dump matches its description. It runs
  with the normal `go test ./...`.

The stack test `test/stack/migrate_test.go` loads `dumps/mariadb.sql` into a MariaDB
next to the compose stack (`test/stack/classic-db.yml`) and migrates it into the
stack's data directory. See [../../docs/classic-migrate.md](../../docs/classic-migrate.md).

## Regenerating

```
task classic:up
task classic:seed
task classic:down
```

Seeding needs a fresh instance, takes a few minutes and creates new random
tokens and timestamps, so the dumps change on every run. Regenerate them only
when the seeded data or the server version changes. To seed one database only,
start it with `docker compose -f test/classic/compose.yml up -d --wait` (add
`--profile postgres` for PostgreSQL) and run `test/classic/seed.sh` with
`CLASSIC_DB=mysql` or `CLASSIC_DB=pgsql`. `CLASSIC_EVENTS` lowers the size of
the big calendar for quick trials, but the committed fixture and its test use
5,000.

## Using the dumps

Load a dump into an empty database:

```
docker exec -i <mariadb container> mariadb -uroot -p<password> <database> < dumps/mariadb.sql
docker exec -i <postgres container> psql -U <user> <database> < dumps/postgres.sql
```

The dumps were taken from the 10.15.3 release. Other versions can have other
column sets.
