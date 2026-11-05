# Backup and restore

All of the service's state is one directory, `GROUPWARE_DATA_DIR` (`/data` in the image
and the Helm chart). Backing it up means copying that directory. There is no database to
dump and no export step. This page says how to copy it safely, how to put it back, how to
check the result, and what the directory does not hold.

## What is in the directory

| Path | What it is |
|---|---|
| `format-version`, `lock` | The layout version and the lock file |
| `owners/<owner>/calendars/`, `owners/<owner>/addressbooks/` | The calendars and address books, one directory per collection, as plain files |
| `owners/<owner>/principal.json` | The user: name, oCIS id, group snapshot, whether the user is disabled |
| `owners/<owner>/app_passwords.json` | The user's app passwords, as bcrypt hashes |
| `owners/<owner>/shares.json`, `shared_with_me.json` | The shares the user made, and the user's own display overrides for collections shared with them |
| `admin.sock` | The admin socket. It exists only while the service runs |

You do not need to back up `admin.sock`. The service replaces a leftover one at start. Files
whose names start with a dot and contain `.tmp-`, and `tmp/` directories inside collections,
are writes that were in progress. The service removes them at start, so it does not matter
whether a backup holds them.

## Two ways to take a backup

### A snapshot of the file system

Take a snapshot of the volume or file system that holds the directory (LVM, ZFS, Btrfs, a
cloud disk snapshot, a CSI `VolumeSnapshot`) and copy from the snapshot. The service can
keep running. A snapshot captures every file at one instant, which is what a crash would
leave behind, and the file store is built to recover from that: it finishes or undoes the
last write of each collection when it loads the collection. A restore from a snapshot is
therefore the same as starting after a power loss.

This only works when the snapshot is atomic for the whole directory. A snapshot per file or
per subdirectory is not.

### A copy of the directory

Stop the service, copy the directory, start the service again.

```sh
# Docker; the image has no shell, so use a throwaway container for the copy
docker stop groupware
docker run --rm -v groupware-data:/data:ro -v "$PWD":/backup alpine \
  tar -C /data --numeric-owner -czf /backup/groupware-data.tar.gz .
docker start groupware
```

```sh
# A directory on the host
systemctl stop ocis-groupware
rsync -a /var/lib/ocis-groupware/ /backup/ocis-groupware/
systemctl start ocis-groupware
```

Calendars and contacts cannot be used while the service is stopped, so pick a quiet hour.
For a directory of the size in [sizing.md](sizing.md) the copy takes seconds to minutes.
Devices that sync during the gap retry on their next poll.

Do not copy the directory while the service runs. A copy made with `cp` or `rsync` takes the
files one after the other. It can catch a log line of a calendar without the file that
belongs to it, or the files of one collection from a different moment than those of
another. The file store then refuses to load that collection when you restore the copy.
The service holds an exclusive lock on the directory (`lock`), but the lock only keeps out
other copies of the service. It does not stop `rsync`.

## How often

The data changes whenever someone saves an event, so a backup is as old as its last run.
Daily is a reasonable start for a team. If losing a day of changes is too much, snapshot
more often. A snapshot is cheap, and the service keeps running.

Keep a copy somewhere other than the disk the data lives on, and try a restore before you
need one.

## Restoring

1. Stop the service. On Kubernetes, scale the Deployment to zero. The directory is locked
   while the service runs, and a restore into a running service is not supported.
2. Move the current directory aside instead of deleting it, in case the backup turns out to
   be the wrong one.
3. Put the backup in its place. For a tar archive, extract it into an empty directory as
   root, so the owner and permissions come back.
4. Check the owner and the mode. The directory and its files must belong to the user the
   service runs as, which is uid 65532 in the image, and be private to that user (the
   store creates directories with mode `0700` and files with `0600`). A restore that
   leaves them owned by root makes the service fail to start, and `/healthz` answers `503`
   when the directory is not writable.
5. Start the service.

The `lock` file can be restored along with everything else. It holds no state.

### What a restore brings back

A restore returns the directory to the moment of the backup, all of it, including the parts
that are not calendar data:

- Events and contacts that were saved after the backup are gone. Devices notice that the
  server no longer has the collection revision they remember, answer with a full sync, and
  show the server's state. A device that still holds a change the server does not have may
  upload it again or drop it, depending on the client. Tell users that changes of the last
  hours before the incident may need to be entered again.
- App passwords created after the backup are gone, so those devices get `401` until the
  user creates a new one.
- App passwords that were revoked after the backup work again.
- A user who was disabled after the backup can sign in again, and a user who was purged
  after the backup is back with all their data.
- The identity lease counts from the `identity_at` stored in the backup. A user who has not
  opened oCIS since then gets `401` on their devices once the lease has run out, until the
  next visit to oCIS Web. See "The identity lease" in the [README](../README.md).

So after a restore, run through your offboarding list again. `ocis-groupware admin
principals list` shows who is disabled, and `disable` is safe to repeat.

## Checking the result

1. The log shows `http server listening` and `admin socket listening`, and no line that
   starts with `skipping unreadable principal file`. That line means a `principal.json` did
   not parse, and that user is missing until the file is fixed.
2. `/healthz` on the debug address answers `200`.
3. `docker exec <container> /ocis-groupware admin principals list` (or `kubectl exec`) lists the users you expect, with the
   expected disabled state.
4. Ask a user to open a calendar over DAV or in the web application. A collection that
   cannot be loaded is reported when it is first used: the request fails and the log names
   the collection directory and the object. The file store never repairs this by itself;
   [its documentation](https://github.com/LukasHirt/groupkit/blob/main/docs/filestore.md#when-a-collection-refuses-to-load)
   describes the two ways out. It happens when a copy was taken from a running service, so
   restoring an older, consistent backup of that collection is usually the quicker fix.
5. Check a shared collection as a recipient. Shares are read from `shares.json` at start. One
   that cannot be parsed is logged and skipped, which means its owner shares nothing until
   the file is fixed.

## What the directory does not hold

The service keeps no copy of what oCIS owns. Back these up the way you back up oCIS:

- The users and groups. They live in oCIS (or its identity provider). The directory holds
  only a snapshot of each user's oCIS id and group ids, which the service refreshes at the
  user's next sign-in. If you restore the service without oCIS, or oCIS from a different
  moment, the oCIS ids in `principal.json` may no longer match anyone. Restore both from the
  same time, or expect users to be provisioned again.
- The service's configuration: the environment or the YAML file, the CA bundle
  (`GROUPWARE_OIDC_CA_FILE`) and, if you imported classic tokens, `GROUPWARE_LEGACY_TOKEN_SECRET`.
  Imported tokens cannot be verified without the secret.
- The ingress or proxy routes ([routing.md](routing.md)) and the oCIS side of the web
  application (`deploy/ocis/web.yaml`).
- Anything held in memory: the cache of verified app passwords and the sign-in failure
  counters. They are rebuilt after a start.

If you use the image from a registry, note the image tag you run. A backup made by a newer
release may not open in an older one.

## Docker and Helm

With Docker, the data is in the volume you mounted at `/data` (`groupware-data` in the
compose stack). `docker run --rm -v groupware-data:/data ...` as above reads it from a
second container. Stop the service container first for a copy. To restore, stop it, empty
the volume or use a new one, and extract the archive into it as root with `--numeric-owner`.

With the [Helm chart](../deploy/helm/ocis-groupware/README.md), the directory is a
PersistentVolumeClaim, and the Deployment runs one replica with the `Recreate` strategy.

- To snapshot, create a `VolumeSnapshot` of the claim. The pod can keep running.
- To copy the files, scale the Deployment to zero, then mount the claim in a temporary pod.
  The claim is `ReadWriteOnce`, so the service pod has to be gone first.
- To restore from a snapshot, create a new claim from it (`dataSource` of kind
  `VolumeSnapshot`), scale the Deployment to zero, set `persistence.existingClaim` to the new
  claim and upgrade the release. Scale up again afterwards.
- `helm uninstall` deletes the claim and the data with it, unless you set
  `persistence.existingClaim` or give the claim the annotation
  `helm.sh/resource-policy: keep` through `persistence.annotations`. Back up before an
  uninstall.
- With `persistence.enabled=false` the data is an `emptyDir` and is lost with the pod. There
  is nothing to back up, and that setup is for trying the chart out.
