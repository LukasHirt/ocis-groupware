# The data directory format and upgrades

This page describes how the service's files are laid out, which of them carry a version,
what a release may change in them, and how to upgrade and roll back. For taking and
restoring copies, see [backup.md](backup.md).

## The layout

The data directory has two kinds of content. The calendars and address books belong to the
file store from [groupkit](https://github.com/LukasHirt/groupkit), which writes them and
documents them in [its file store documentation](https://github.com/LukasHirt/groupkit/blob/main/docs/filestore.md):
the object files, `collection.json`, the change log, the file name encoding and the crash
recovery. That page is the reference for everything below `calendars/` and `addressbooks/`.
The service adds a few files of its own next to them.

```
data/
├── format-version             the file store's layout version
├── lock                       held while the service runs
├── admin.sock                 the admin socket, only while the service runs
└── owners/
    └── <owner>/
        ├── principal.json          the user (service)
        ├── app_passwords.json      hashes of the user's app passwords (service)
        ├── shares.json             what the user shares (service)
        ├── shared_with_me.json     the user's overrides for shared collections (service)
        ├── calendars/<collection>/…      (file store)
        └── addressbooks/<collection>/…   (file store)
```

`<owner>` is the owner key of the user, encoded as a file name by the file store's rule
(upper case letters, `@` and other bytes become `%XX`). It is the identity provider's
subject for a user who signed in, and a name of its own for a user imported before the
first sign-in.

## What carries a version

| File | Version | When the service meets another version |
|---|---|---|
| `format-version` | The integer `1` and a newline | The service does not start. The error names the version found and the one this build reads |
| `shares.json`, `shared_with_me.json` | `"version": 1` inside the file | The file is logged and skipped. The user shares nothing, or sees the owner's names and colours, until the file is fixed or replaced |
| `principal.json`, `app_passwords.json` | None | Fields are optional. A file without a newer field counts as having its default |

The file store does not look at the files of the service, and the service does not look at
the store's. The version in `format-version` covers only what the file store writes.

## The policy

- Within a major version the format only grows. A release may add a field to a JSON
  file of the service and read older files without it. It does not rename or remove a field,
  and does not change what a field means.
- A change that cannot be made that way raises a version. For the file store that is
  `format-version`. For `shares.json` and `shared_with_me.json` it is their `version`.
  `principal.json` and `app_passwords.json` have no version of their own, so a change to
  them that is not additive would get one first. Such a release says so in its notes and
  gives the steps to move the data.
- A build refuses what it does not know. A build never opens a data directory with a
  `format-version` it does not read. It leaves the directory untouched, and does not keep
  the lock.
- There is no silent conversion. A release that needs the data changed does it in a
  step the operator runs, with the service stopped, and says so in its release notes. At
  the time of writing no release has needed one: the format is at version 1.

These refusals are tested. The file store's tests cover `format-version`, the service's
tests cover that a start on a directory with an unknown version exits with an error and
changes nothing, and the sharing tests cover the version of `shares.json` and
`shared_with_me.json`.

### Going back to an older release

An older build does not read the newer fields. It ignores them, and when it writes the
file again, they are gone. For example a release before the `disabled` field existed
lets a disabled user sign in again, because it does not know the field. A downgrade is
therefore not a safe way back, and for the same reason the file store's `format-version`
check is not a promise that every downgrade is caught: a release that only added fields
keeps `format-version` at 1.

The way back is the backup you took before the upgrade, with the older image. Restore the
copy as described in [backup.md](backup.md) and start the old release on it. Do not start
an older release on a directory a newer release has written to.

## Upgrading

1. Read the release notes of every release between yours and the target. They list changes
   to the format and to the defaults.
2. Take a backup of the data directory ([backup.md](backup.md)). A snapshot is enough.
3. Stop the old service and start the new one on the same directory. In Docker, pull the new
   image and recreate the container. With Helm, `helm upgrade`: the chart's `Recreate`
   strategy stops the old pod before the new one starts, so the lock is free.
4. Check the log for `skipping unreadable principal file`, `cannot understand shares` and
   `cannot understand shared collection overrides`, and `/healthz` on the debug address.
5. Keep the backup until you are sure the new release works for your users.

Starting a release that reads a different format than the directory has stops with an
error that names the version, and changes nothing. In that case start the release you
came from, or restore the backup.
