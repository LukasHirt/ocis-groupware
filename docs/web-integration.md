# How the web application docks into oCIS Web

This page records what oCIS Web exposes to an application that it loads at
run time, and what was checked against a real stack. The code in `web/` is
built on these facts, so when a new Web version changes one of them, this
page and the build change together.

## Verified against

| What | Version |
|---|---|
| oCIS | 8.2.2 (image `owncloud/ocis:8.2.2`) |
| ownCloud Web shipped in it | 12.5.0 |
| `@ownclouders/web-pkg`, `web-client`, `extension-sdk` | 12.5.0 |
| Browser | Chromium (headless shell) through Playwright 1.64.1 |

The check runs in CI on every pull request: the stack job boots oCIS and
ocis-groupware with docker compose, once with the ingress routing the
groupware paths and once with the oCIS proxy forwarding them, and the browser
test in `web/tests/e2e` signs in as a demo user and uses the application
inside oCIS Web. It was also run by hand on macOS against the same stack on
2026-11-05.

Only oCIS 8.x is supported. The ownCloud packages are pinned to the Web
version that the supported oCIS release ships, and Dependabot leaves them
alone; they are bumped by hand when the supported oCIS version moves.

## How Web loads the application

The web service's configuration lists the application under
`web.config.external_apps`, which Web receives in `config.json`:

```yaml
web:
  config:
    external_apps:
      - id: organizer
        path: /web/groupware.js
```

The `id` of an entry is a label; the application's own id, `organizer`, comes
from its `appInfo`. The file keeps the name `groupware.js`.

The compose stack mounts `deploy/ocis/web.yaml` with that content as
`/etc/ocis/web.yaml`. Web joins a relative `path` with its own server URL and
loads it with requirejs, since the name ends in `.js`. A path ending in
`.mjs` or `.ts` would be loaded as an ES module instead, which cannot work
here: the modules the host provides are registered with requirejs, not with
an import map.

Web's content security policy allows scripts from its own origin only
(`script-src 'self'`), so the entry must be served on the oCIS domain. That
is why ocis-groupware sits behind the same ingress or the oCIS proxy.

## What the host provides

Web registers these modules with requirejs before it loads applications, and
the extension SDK marks them as externals, so the bundle refers to them by
name and contains none of their code:

- `vue`
- `pinia`
- `luxon`
- `vue3-gettext`
- `@ownclouders/web-pkg` (also as `web-pkg`)
- `@ownclouders/web-client`, with its `graph`, `graph/generated`, `ocs`,
  `sse` and `webdav` entry points (also as `web-client`)

`vue-router` is not provided. The application imports only its types and
uses the router composables of `web-pkg`; an ESLint rule in `web/` refuses a
value import. The design system's components (`oc-button` and the like) are
registered globally by the runtime and need no import.

The built entry is an AMD module whose dependency list is exactly `vue`,
`vue3-gettext` and `@ownclouders/web-pkg` at the moment.

## The application contract

The entry's default export is `defineWebApplication({ setup })` from
`web-pkg`. The runtime takes the AMD module's value as the application
script, so the module must have no other export; the build enforces that
with `exports: 'default'`. `setup` runs inside the host's Vue app context,
so composables such as `useGettext` work there. It returns:

- `appInfo` with a required `id` and `name`. Routes are mounted below
  `/:scope(vault)?/<id>`. The application names its routes `<id>-...` itself.
  The id is `organizer`. It must not be a path that the ingress or the oCIS proxy
  sends to this service (`groupware`, `dav`, `graph`, `web`, `remote.php`,
  `.well-known`): a reload or a bookmark of an application page asks the server
  for that URL, and the service would answer it instead of oCIS Web. The first
  id, `groupware`, had this problem.
- `routes`, plain vue-router records. `meta.authContext: 'user'` makes Web
  require a signed-in user.
- `navItems`, which become the sidebar of the application (an extension
  point named `app.<id>.navItems`).
- `extensions`, a computed list. An extension of type `appMenuItem` with a
  `path` puts the application into the application switcher, rendered as a
  link. Without it the application has routes but no entry in the menu.
  The menu is built from extensions, not from applications, so one
  application can add several entries.
## Calendar and Contacts as two menu entries

One application serves two areas, and `extensions` holds one `appMenuItem` for
each: Calendar (`organizer.calendar.app-menu-item`, icon `calendar`, path
`/organizer/calendar`) and Contacts (`organizer.contacts.app-menu-item`, icon
`contacts-book`, path `/organizer/contacts`). Web marks a menu entry as active
when the current path starts with its path, so each area highlights its own.

The pages sit below `/organizer/calendar/` and `/organizer/contacts/`, since
Web mounts every route of an application below its id. Each area has two
navigation items, its main page and Devices, which shows the DAV address and the
app passwords and is the same page in both areas. A navigation item has an
`isVisible` function that Web evaluates for the current route, so the items of
the other area are hidden. The area comes from the route name
(`web/src/composables/useArea.ts`). The mobile navigation uses the same list.

An administrator can hide one entry with `disabledExtensions` in the Web
configuration, for example `organizer.contacts.app-menu-item`. That hides the
link only; the pages stay reachable by their URL.

Two more fields of the contract carry the application's look and language.
`appInfo.img` is an image address; the application passes its icon as a data
URI, built from `web/src/assets/icon.svg`, so no extra file has to be served or
cached. The application switcher does not use it: Web 12.5.0 draws a menu entry
from a named icon of its design system on a gradient made from the extension's
`color`, so the entry keeps the `calendar` icon and takes the icon's blue as its
colour. `translations`, a map of language to messages, is merged into Web's
catalog; [translations.md](translations.md) describes how it is produced.

## Calling the service from the browser

`useClientService().httpAuthenticated` from `web-pkg` is an axios client
that sends the signed-in user's bearer token and refreshes it when it
expires. The token itself is `useAuthStore().accessToken`. The browser is
on the oCIS origin, so requests to `/groupware/v1/...` and the Graph paths
need no CORS handling.

The application wraps this in `web/src/composables/useApi.ts`. It binds the
typed client in `web/src/api` to `httpAuthenticated` and offers `run()`, which
reports a failed call with `useMessages().showErrorMessage`, so errors appear
as Web's own toasts. Both composables come from `web-pkg` and are external
like the rest of it. `httpAuthenticated` has no base URL, so the client uses
absolute paths below `/graph/v1.0/me`. A status that is not 2xx makes it
throw an axios error; the client turns that into an `ApiError` with the code
and message of the Graph error body.

The sharing dialog also reads the oCIS directory, with the same client. `GET
/graph/v1.0/users?$search=<text>` and `/groups?$search=<text>` work with the
signed-in user's own token and need at least three characters; a user who is not an
administrator gets the id and display name of each match, and cannot list a group's
members. The paths are served by oCIS itself, since the ingress routes only
`/graph/v1.0/me/...` to this service, so no extra route is needed. The dialog does
not fall back to typing an id, because the search is available to every signed-in user.

## Build output and caching

The SDK's production build names the entry with a content hash. The build
in `web/` overrides that to `groupware.js`, because the path in
`external_apps` is fixed, and names chunks (`chunks/<name>-<hash>.mjs`) and
assets (`assets/<name>-<hash><ext>`) with a hash. The server serves
`groupware.js` with `Cache-Control: no-cache` and everything else as
immutable for a year.

## Licences

The packages the application builds against are externals: none of their
code is in the shipped bundle, and none is in the image. The npm artifacts
`@ownclouders/web-pkg`, `@ownclouders/web-client` and
`@ownclouders/extension-sdk` at 12.5.0 declare `AGPL-3.0` in their package
metadata, while the same packages in the oCIS 8.2.2 source tree declare
Apache-2.0. The `@ownclouders/web-test-helpers` package, which ownCloud's
own extensions use in unit tests, is not used here; the unit tests mock the
two host modules they touch.

## Running the browser test by hand

With the compose stack up and Node 24:

```sh
cd web
npm ci
npx playwright install chromium
STACK_URL=https://ocis.localhost npm run test:e2e
```

`STACK_URL` defaults to `https://ocis.localhost`. The test ignores
certificate errors, since the stack's certificate is self-signed.
