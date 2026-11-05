# Sizing

This page says how large an installation the service has been measured at, what a
restart costs, and how to repeat the measurement. Everything below is a figure from one
run on one machine. Nothing is extrapolated: a size that is not in the tables was not
tried.

## What was measured

The service handled the following without errors, with the latencies in the tables below:

| Dimension | Size measured |
|---|---|
| Principals | 1,000, each with an app password and a calendar of 10 events |
| Largest calendar | 30,000 events, with calendars of 10,000 and 1,000 events next to it |
| Shares | 20 calendars, each shared with 50 users; the 10,000 event calendar is shared with 50 users as well |
| Sync clients | 200 clients, each polling every 30 seconds for 5 minutes |
| Writes during the polling | 2 new events per second, spread over the clients' calendars |

The calendars hold about 5 percent recurring events (weekly, 10 to 49 occurrences), so
a range query has to expand some of what it matches.

## The machine and the method

| | |
|---|---|
| Machine | Apple M4 Max, 14 cores, 36 GB, macOS 26.6.2, APFS |
| Go | 1.26.10, `darwin/arm64` |
| Service | the binary built from this repository, one process, log level `warn` |
| Date | 2026-11-05 |

The machine was a development laptop, not a dedicated host, and other programs may have
run on it during the measurement. An earlier run of the same load, on a busier machine
and on a service build from a few changes before, gave slower figures: a range query
on 10,000 events had a p95 of 266 ms instead of 176 ms, and the first request on the
30,000 event calendar took 7.9 seconds instead of 0.8. The tables below are the later,
quieter run. Expect this spread between runs.

The load generator in `test/load` and the service ran on the same machine and talked
over loopback without TLS. There was no identity provider, no oCIS and no ingress. The
generator wrote the data directory itself with the packages the service is built from,
issued an app password for each principal (bcrypt cost 10) and gave each principal a fresh identity snapshot, so that the identity lease lets the Basic sign-ins through, then started the binary on
that directory and signed in with HTTP Basic. The first sign-in of each principal pays
for bcrypt, later ones hit the credential cache.

Latencies are what the client sees from sending the request to reading the whole
response. Memory is the resident set of the service process, read with `ps` once a
second. Disk use is the size of the data directory after seeding.

The figures leave out the Graph API, the web application, address books, import, the
sign-in with an oCIS token, network latency and TLS. Each of those adds cost that this
run did not measure.

## Results

### Cold start

The service starts without reading any calendar. It reads each principal's file and the
share files, and loads a calendar the first time a request needs it. So a start has two
costs: the time until `/healthz` answers, and the first request on each large calendar.

| | Healthy after | First range query on 1,000 events | on 10,000 events | on 30,000 events |
|---|---|---|---|---|
| First start on the seeded directory | 763 ms | 80 ms | 269 ms | 782 ms |
| Restart after the load | 264 ms | 73 ms | 227 ms | 823 ms |

Only the first request on a calendar pays this; the later ones in the table below do
not. A calendar stays in memory until the service stops. The first request is a range
query, so its time includes answering the query as well as loading the calendar: a
steady range query on 10,000 events takes about 170 ms (see below), which leaves about
50 to 100 ms for the load.

### Memory

| After | Resident set |
|---|---|
| The start and one query on each of the three large calendars | 55 MB |
| 100 range queries and the syncs on each large calendar | 75 MB |
| The shares and the 5 minute sync storm | 78 MB |

The data directory held 55,000 events and used 28 MB on disk. The memory right after
the start already includes the three large calendars, because the first query loads
them; the footprint of an idle service with nothing loaded was not measured separately.

### Latency

Range query is a CalDAV `calendar-query` with a one week time range, one request at a
time, asking for the event data. Full sync is `sync-collection` without a token, delta
sync is one with the token of the previous reply after one new event.

| Operation | Count | p50 | p95 | p99 | max |
|---|---|---|---|---|---|
| Range query, 1,000 events | 100 | 15 ms | 15 ms | 18 ms | 41 ms |
| Range query, 10,000 events | 100 | 169 ms | 176 ms | 195 ms | 218 ms |
| Range query, 30,000 events | 100 | 590 ms | 615 ms | 641 ms | 660 ms |
| Full sync, 1,000 events | 6 | 3.5 ms | 4.2 ms | 4.2 ms | 4.2 ms |
| Full sync, 10,000 events | 6 | 32 ms | 33 ms | 33 ms | 33 ms |
| Full sync, 30,000 events | 6 | 98 ms | 102 ms | 102 ms | 102 ms |
| Delta sync, 10,000 events | 20 | 1.5 ms | 3.9 ms | 4.1 ms | 4.1 ms |
| Delta sync, 30,000 events | 20 | 5.3 ms | 7.6 ms | 7.8 ms | 7.8 ms |
| Add one event, 10,000 event calendar | 20 | 12 ms | 14 ms | 14 ms | 14 ms |
| Add one event, 30,000 event calendar | 20 | 12 ms | 15 ms | 15 ms | 15 ms |
| Calendar home listing with shared calendars | 50 | 67 ms | 78 ms | 81 ms | 81 ms |
| Full sync of the shared 10,000 event calendar by a recipient | 50 | 43 ms | 55 ms | 57 ms | 57 ms |

A range query costs time in proportion to the size of the calendar, about 17 to 20 µs
per event at p50 here. The sync requests do not follow the size that closely: a delta
sync that finds one change took a few milliseconds on 10,000 and on 30,000 events. A
range query on 10,000 events was under 200 ms at the 99th percentile and under a
second on 30,000 events.

### Sync storm

200 clients, each polling its own calendar and the calendars shared with it every 30
seconds, with the 50 clients that receive the large shared calendar also running a
range query on it each time. Each client started at a random point of the first 30
seconds and did one full sync before its first poll.

| Operation | Count | p50 | p95 | p99 | max |
|---|---|---|---|---|---|
| Poll of the client's own calendar (delta sync) | 1,800 | 0.5 ms | 1.3 ms | 1.7 ms | 53 ms |
| Poll of a shared calendar (delta sync) | 8,595 | 0.3 ms | 0.7 ms | 0.9 ms | 5.4 ms |
| Range query on the shared 10,000 event calendar | 450 | 230 ms | 304 ms | 363 ms | 407 ms |
| First sync of a client | 1,155 | 0.9 ms | 68 ms | 96 ms | 104 ms |
| Event written during the storm | 599 | 15 ms | 28 ms | 77 ms | 113 ms |

No request failed in the run.

## What the figures support

At these sizes on this machine, a single service process answered every operation above
in under a second at the 95th percentile, and the sync polling that dominates real
client traffic stayed in single milliseconds. The measured sizes are:

- 1,000 principals
- one calendar of 10,000 events, and calendars up to 30,000
- 50 recipients on a shared calendar, and 20 such calendars
- 200 clients polling every 30 seconds

The largest calendar is where the margin is thinnest: a range query on 30,000 events
had a p95 of 615 ms in this run and 815 ms in the busier earlier one. Clients that ask
for large ranges on calendars of that size, or many such clients at once, are not
covered by this run. Past what is in the tables, the
service has not been tried.

Every figure is for one process on one data directory, which is how the service runs
(the directory is locked, and the Helm chart installs one replica).

## Repeating the measurement

The load generator lives in `test/load` and needs only a Go toolchain. It builds the
service itself unless it is given a binary with `-bin`.

```sh
go run ./test/load -sizes 10000,1000,30000 -duration 5m -json load.json
```

The defaults are 1,000 principals, one calendar of 10,000 events, 50 recipients per
shared calendar and 200 clients polling every 30 seconds for 2 minutes. `go run ./test/load -h` lists
every flag. The calendar named first in `-sizes` is the one that is shared. Seeding is
the slow part, because every event is written with the store's flush guarantees: it
took 14 minutes for the run above on this machine, and it is much quicker on a Linux
file system. The data directory is deleted at the end unless `-keep` is given.

A workflow runs the generator every night and on request, and attaches the output to
the run. It only reports. It is not a required check and a slow figure does not fail
it.
