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
      - id: groupware
        path: /web/groupware.js
```

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
  `/:scope(vault)?/<id>` and route names are prefixed with `<id>-`.
- `routes`, plain vue-router records. `meta.authContext: 'user'` makes Web
  require a signed-in user.
- `navItems`, which become the sidebar of the application (an extension
  point named `app.<id>.navItems`).
- `extensions`, a computed list. An extension of type `appMenuItem` with a
  `path` puts the application into the application switcher, rendered as a
  link. Without it the application has routes but no entry in the menu.

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
