# Mapping classic users and groups

A classic installation identifies people by a login name and groups by a name.
oCIS identifies both by an opaque id. The package `internal/migrate/idmap`
turns the first into the second for the users and groups a migration reads.

## Sources

1. **CSV files.** Two columns: the classic id and the oCIS id. A first line
   that starts with `classic_id`, blank lines and lines starting with `#` are
   skipped. An id that appears twice is an error. Users and groups use separate
   files.

   ```
   classic_id,ocis_id
   alice,4c510ada-c86b-4815-8820-42cdf82c3d51
   ```

2. **The oCIS Graph API.** For ids that no CSV file lists, the mapper asks oCIS
   with an administrator's access token:

   - users: `GET /graph/v1.0/users?$search="<login>"`, keeping only users whose
     `onPremisesSamAccountName` is exactly the classic login. oCIS answers a
     `$filter` on that property with 501, and the search also matches parts of
     names.
   - groups: `GET /graph/v1.0/groups?$search="<name>"`, keeping only groups whose
     display name is exactly the classic group id, because the search also
     matches parts of names.

   The classic login must be the user's `onPremisesSamAccountName`, which is the
   case when oCIS reads its users from the same LDAP directory as the classic
   server. Quotes in a login are escaped.

CSV entries win over the directory. A migration that only has CSV files never
contacts oCIS.

## What is reported

An id is left unmapped, with a reason, when no source knows it (`not found`) or
when the directory returns more than one match (`more than one match`). The
mapper never picks one of several. Unmapped ids come back in the result in the
order they were given, users first, so a report can list them.

A failure to ask the directory, such as a refused token, a network error or an
unexpected answer, stops the run with an error. Reporting every remaining id as
missing would hide the real problem.
