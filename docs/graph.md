# Graph API reference

The service answers a part of the Microsoft Graph calendar and contacts API below
`/graph/v1.0/`, and four endpoints that Graph has no equivalent for below `/groupware/v1/`.
[graph-openapi.yaml](graph-openapi.yaml) lists every path with its parameters and shapes. This
page adds what a schema cannot say: how the stored data maps to Graph fields, what that mapping
loses, and what the query options accept.

Data lives in iCalendar and vCard objects, because CalDAV and CardDAV clients use the same
objects. The facade reads and writes them and never keeps a second copy. Two rules follow from
that and hold for every field below:

- Reading never changes an object.
- Writing edits the parsed object. A field the request does not send, and every property the
  mapping has no field for, stays as it was.

## Keeping the document true

`internal/graphapi` has tests that read `graph-openapi.yaml`. They fail when

- a route has no entry in the document, or an entry has no route;
- a contract fixture in `internal/graphapi/testdata/contract` does not fit its schema, whether
  by a missing required field, a wrong type or a field the schema does not name;
- the properties or required fields of a schema differ from the Go type the handler writes, or
  the properties of a request schema differ from the type the handler reads;
- a list documents `$orderby` or `$filter` that it does not serve, or the other way round.

Change a response shape, regenerate the fixtures with `go test ./internal/graphapi -update`, and
edit the schema to match. The document does not describe error bodies beyond their shape, and it
does not list the `405` that any known path gives for an unknown method.

## Identifiers and validators

| Item | Identifier |
|---|---|
| Calendar, contact folder | the URL-safe base64 form, without padding, of the collection id. The owner and everyone the collection is shared with see the same id |
| Event, contact | the collection id, a dot, and the URL-safe base64 form of the object's name in the collection |
| Occurrence of a series | the event id, a dot, and the `RECURRENCE-ID` in UTC, such as `20261120T090000Z` |
| Permission | an opaque id that names the grantee |

`@odata.etag` is the object's ETag as a weak validator, and `changeKey` is the bare value.
`PATCH` and `DELETE` honour `If-Match` (`*`, the weak form or the bare form) and answer `412`
with the code `preconditionFailed`. Whatever the header says, a write is checked against the
ETag it read, so two writers cannot overwrite each other without notice.

## Query options

| Option | Where | What it accepts |
|---|---|---|
| `$top` | every list | 1 to 1000, default 100. Anything else is `400` |
| `$skip` | every list | 0 or more |
| `$select` | every list and every single item | top-level property names separated by commas. `id` and `@odata.etag` stay in the answer. An unknown name is `400`, and a nested name such as `start/dateTime` is `501` |
| `$orderby` | event lists, `calendarView` and contact lists | one property and an optional `asc` or `desc` |
| `$filter` | event lists and `calendarView` | the grammar below |

The options apply in this order: `$filter`, `$orderby`, the page cut by `$skip` and `$top`,
then `$select`. `@odata.nextLink` repeats the request with every option and a moved `$skip`.

Properties that `$orderby` takes:

- events: `subject`, `start/dateTime`, `end/dateTime`, `createdDateTime`, `lastModifiedDateTime`
- contacts: `displayName` (the default), `givenName`, `surname`, `companyName`,
  `lastModifiedDateTime`

Text sorts without regard to case, and ties are broken by id so a page boundary is stable. A
property the resource has but the list does not order by, and more than one property, answer
`501`. A property the resource does not have answers `400`.

### Event filters

```
filter     = condition *( "and" condition )
condition  = time-prop time-op time
           / "subject" "eq" string
           / ( "startswith" / "contains" ) "(" "subject" "," string ")"
time-prop  = "start/dateTime" / "end/dateTime"
time-op    = "ge" / "gt" / "le" / "lt"
```

A time is an RFC 3339 value or a date, quoted or bare. Without an offset it is UTC. It names an
instant, so the zone that events are shown in (`Prefer: outlook.timezone`) does not change which
events match. A string is in single quotes, with a quote inside written twice. Keywords and
function names are not case sensitive, and string comparison ignores case.

A series in `/events` is one item and matches by its first occurrence. In `calendarView` every
occurrence is tested on its own.

An expression that is valid OData but outside the grammar answers `501`: `or`, `not`,
parentheses, `ne`, `eq` on a time, other properties and other functions. Text that is not valid
answers `400`.

### Everything else

`$expand`, `$count`, `$search`, `$skiptoken`, `$batch`, `delta`, `/instances`, attachments,
child folders and `$filter` on anything but event lists answer `501` with the code
`notImplemented`, so a client can tell an unsupported request from a missing item. So does any
path the service does not route.

## Event mapping

| Graph | iCalendar | What is lost or changed |
|---|---|---|
| `subject` | `SUMMARY` | |
| `body` | `DESCRIPTION` | Plain text only. Writing `html` is `400`. |
| `location.displayName` | `LOCATION` | Only the name. A Graph location object has more fields, and `locations` is not offered. |
| `start`, `end`, `isAllDay` | `DTSTART`, `DTEND` or `DURATION` | A floating time reads as UTC. A `DURATION` is returned as an end. |
| `showAs` | `TRANSP`, `STATUS` | `free` is transparent, `tentative` is `STATUS:TENTATIVE`, everything else is busy. `oof`, `workingElsewhere` and `unknown` are stored as busy and read back as `busy`. A `STATUS:CANCELLED` event keeps its status. |
| `sensitivity` | `CLASS` | `normal` writes `PUBLIC` where the object already has a `CLASS`. `private` and `confidential` map to `PRIVATE` and `CONFIDENTIAL`. `personal` is stored as `PRIVATE` and reads back as `private`. `CLASS` values other than these read as `normal`. |
| `categories` | `CATEGORIES` | |
| `attendees` | `ATTENDEE` | `emailAddress`, `type` and `status.response` map to `CN`, `ROLE`/`CUTYPE` and `PARTSTAT`. No invitation or update is sent. |
| `organizer` | `ORGANIZER` | Read only. |
| `isReminderOn`, `reminderMinutesBeforeStart` | `VALARM` | Only the first display alarm with a trigger before the start. Other alarms stay in the object and are not shown. |
| `recurrence` | `RRULE` | See below. |
| `isCancelled` | `STATUS:CANCELLED` | Read only. |
| `iCalUId` | `UID` | Read only. |
| `createdDateTime`, `lastModifiedDateTime` | `CREATED`, `LAST-MODIFIED` | Read only. Absent when the object has neither. |

Fields that Graph has and the facade neither reads nor writes: `onlineMeeting` and the other
online-meeting fields, `importance`, `responseRequested`, `hasAttachments`, `attachments`,
`extensions`, `webLink`, `originalStart` and `transactionId`. They are not in the answers. A
request that sends one is accepted and the field is ignored, as Graph does for fields it does
not know.

### Time zones

`start` and `end` take IANA names and Windows names. On a single event a Windows name is
converted and stored as the UTC time. On a recurring event it is kept as the zone of the rule, so
the rule keeps its local time across a change of offset. Answers show times in UTC, or in the
zone of `Prefer: outlook.timezone="..."`, which the answer repeats in `Preference-Applied`. An
unknown zone is `400`. An all-day event has a date at midnight in the zone the answer uses.

### Recurrence

`recurrence` covers `daily`, `weekly`, `absoluteMonthly`, `relativeMonthly`, `absoluteYearly`
and `relativeYearly` patterns, with the range types `noEnd`, `endDate` and `numbered`.

A rule the pattern cannot express comes back as `recurrence: null` with
`"groupware.recurrenceEditable": false`. That covers rule parts outside `FREQ`, `INTERVAL`,
`COUNT`, `UNTIL`, `BYDAY`, `BYMONTHDAY`, `BYMONTH`, `BYSETPOS` and `WKST` (`BYHOUR` and
`BYWEEKNO`, for example), combinations the pattern has no field for, an `INTERVAL` above 99, a
`COUNT` together with an `UNTIL`, and an object with more than one `RRULE` or with an `EXRULE`.
The rule stays in the object and keeps expanding in `calendarView`. A `PATCH` that sends
`recurrence: null` for such an event leaves the rule alone. An object with `RDATE` entries is
expanded in `calendarView` too.

Edits to occurrences follow Graph's meaning of "this event":

- `PATCH` on an occurrence id writes a `RECURRENCE-ID` override and leaves the series alone. It
  cannot carry `recurrence`.
- `DELETE` on an occurrence id adds an `EXDATE`.
- `DELETE` on the master removes the object with its series and overrides.
- An occurrence that the series does not have, or that was excluded, is `404`.

`type` is `singleInstance`, `seriesMaster`, `occurrence` or `exception`. Events in `/events` are
the object's master. `calendarView` expands them and returns occurrences and overrides, each with
its own id and a `seriesMasterId`.

## Contact mapping

| Graph | vCard | What is lost or changed |
|---|---|---|
| `displayName` | `FN` | |
| `surname`, `givenName`, `middleName`, `title`, `generation` | `N` | The five components of `N`. |
| `nickName` | `NICKNAME` | The first value. |
| `yomiGivenName`, `yomiSurname`, `yomiCompany` | `X-PHONETIC-FIRST-NAME`, `X-PHONETIC-LAST-NAME`, `X-PHONETIC-ORG` | |
| `companyName`, `department` | `ORG` | The first two components. |
| `jobTitle`, `profession` | `TITLE`, `ROLE` | |
| `birthday` | `BDAY` | A birthday without a year is year 0001 in Graph and `1604` with `X-APPLE-OMIT-YEAR` in cards from Apple clients. Both are read and written. |
| `emailAddresses` | `EMAIL` | `name` repeats the address, and the card's types and labels are not shown. A duplicate address is shown once. |
| `mobilePhone` | `TEL` of type `cell`, or labelled mobile | The first match. Further mobile numbers are not shown. |
| `businessPhones` | `TEL` of type `work` | |
| `homePhones` | `TEL` of type `home`, and every number without a place | A fax, pager, video or text phone number is not shown. |
| `homeAddress`, `businessAddress`, `otherAddress` | `ADR` of type `home`, `work` and the rest | The first address of each kind. Street, city, state, postal code and country map to the `ADR` parts; the post office box and the extended address are not shown. |
| `businessHomePage` | `URL` | The first one. |
| `imAddresses` | `IMPP`, `X-SOCIALPROFILE` | Shown as the stored URIs, once each. |
| `personalNotes` | `NOTE` | |
| `categories` | `CATEGORIES` | Contact groups made in a client are categories. |
| `assistantName`, `manager`, `spouseName`, `children` | `X-ABRELATEDNAMES`, `RELATED`, `X-ASSISTANT`, `X-MANAGER`, `X-SPOUSE` | Written as `X-ABRELATEDNAMES` with a label. |
| `groupware.groups` | group cards that list the contact | Read only. A card with `KIND:group` or `X-ADDRESSBOOKSERVER-KIND` never appears as a contact, is `404` by id, and is never written. |
| `groupware.hasPhoto` | `PHOTO` | True when the card embeds an image. A photo that is only a link is not shown. |

A card is written as vCard 3.0 when the facade creates it, and an existing card keeps its
version. Photos are JPEG or PNG up to 1 MB, stored as `PHOTO;ENCODING=b` in 3.0 and as a `data:`
URI in 4.0.

Contact fields of Graph that are not in the table, such as `fileAs`, `flag` and `extensions`, are not read or written. Whatever a card holds beyond the table stays in the card.

## Calendars, contact folders and permissions

A calendar has Graph's `name`, `color`, `hexColor`, `isDefaultCalendar`, `canShare`,
`canViewPrivateItems`, `canEdit`, `isRemovable` and `owner.name`. `color` is always `auto`, and
`hexColor` is the `#rrggbb` the user chose or empty. The default calendar is the oldest one the
user owns. `/me/events` and `POST /me/events` use it, as Graph does, while `/me/calendarView`
spans every calendar the user sees.

A contact folder has `id` and `displayName` and, where Graph has no field, four extension
fields: `groupware.isDefault`, `groupware.canEdit`, `groupware.canShare` and `groupware.owner`.
Extension fields start with `groupware.` so a Graph client can ignore them.

Permissions have Graph's `role` (`read` or `write`, the only two a share can give),
`allowedRoles`, `isRemovable`, `isInsideOrganization` and `emailAddress.name`. Graph names the
grantee by e-mail address, which not every user has, so the permission adds
`grantee: {type: "user" | "group", id}`, and a `POST` takes `role` and `grantee`. The
`emailAddress.name` is the display name the grantee had when the share was made. Graph's other
roles, such as `delegateWithPrivateEventAccess`, do not exist here.

## Endpoints outside Graph

| Request | Answer |
|---|---|
| `GET /groupware/v1/me` | The user's login name and the DAV addresses to enter in a client |
| `POST /groupware/v1/calendars/{id}/import` | Imports an iCalendar file. Up to 20 MB |
| `GET /groupware/v1/calendars/{id}/export` | One iCalendar file with every event, sent as an attachment |
| `POST /groupware/v1/addressbooks/{id}/import` | Imports a vCard file. Up to 20 MB |
| `GET /groupware/v1/addressbooks/{id}/export` | One vCard file with every card, sent as an attachment |

An import answers with a report that has one entry per object in the file: its position, its
UID, the name it got in the collection, and `created`, `updated` or `failed` with a reason. An
object with the UID of a stored one replaces it in place. A bad object does not fail the file.
A read-only collection is `403`. The service runs at most one import per user and four at
once; another one is `429` with `Retry-After: 5`.

## Calling the API from a script

`test/stack/graph_script.py` is a script that uses nothing but the Python `requests` package. It
creates events and contacts, reads them back with paging, `$select`, `$orderby` and `$filter`,
changes one of each with `If-Match`, checks that unsupported options answer `501`, and deletes
what it made. It is a short example of the calls an integration needs.

```
GRAPH_URL=https://ocis.example/graph/v1.0 GRAPH_TOKEN=<access token> python graph_script.py
```

Set `REQUESTS_CA_BUNDLE` when the server has a certificate that the system does not trust. The
stack tests run the script against the compose stack when `STACK_PYTHON` names an interpreter
that has `requests` installed, which the interpreter used for the CalDAV client check does.
