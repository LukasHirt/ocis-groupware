# Routing

ocis-groupware lives on the same domain as oCIS. The web app runs inside oCIS Web and
calls the service with the browser's token, DAV clients discover the service through
`/.well-known/caldav` and `/.well-known/carddav` on the oCIS address, and the service
validates tokens against the oCIS identity provider under that address. So something
in front of oCIS has to send a fixed set of paths to the service and everything else to
oCIS. This page lists those paths, gives snippets for common ingresses, and records
what happens when the proxy inside oCIS is asked to do the job instead.

## The paths

| Path | What it is |
|---|---|
| `/dav` and `/dav/` | The DAV root, where clients find their principal |
| `/dav/calendars/`, `/dav/addressbooks/`, `/dav/principals/` | CalDAV and CardDAV |
| `/.well-known/caldav`, `/.well-known/carddav` | Client discovery (RFC 6764) |
| `/graph/v1.0/me/calendar…`, `/graph/v1.0/me/events…`, `/graph/v1.0/me/contact…` | The calendar and contacts part of the Graph API |
| `/groupware/` | The service's own API, for example app passwords |
| `/web/` | The web app that oCIS Web loads |

Only these go to the service. `/dav/spaces/`, `/dav/files/`, `/remote.php/`, the rest of
`/graph/` and everything else stay with oCIS. The matches are on the path only; the
service does not need the ingress to rewrite anything or add headers.

`/healthz` and `/metrics` are for the monitoring side. The development stack routes them
through Traefik so the stack tests can wait for the service, and the snippets below leave
them out. Reach them on the service's own address.

## Ingress snippets

The supported setup is an ingress in front of oCIS that routes the paths above. The
snippets in `deploy/` do that for:

- Traefik: `deploy/traefik/ocis-groupware.yml` (file provider). `docker-compose.yml`
  carries the same rule as container labels and is what CI runs.
- nginx: `deploy/nginx/ocis-groupware.conf`. Exact and `^~` prefix locations for the
  DAV paths and a regex location for the Graph paths; the regex is only reached when no
  `^~` location covers `/graph/`.
- Caddy: `deploy/caddy/Caddyfile`. Two named matchers and a fallback `handle`.

All three use the same path list. The DAV paths need the full set of WebDAV methods
(PROPFIND, PROPPATCH, REPORT, MKCOL, MKCALENDAR, PUT, DELETE, MOVE and COPY), which
these three pass through as they are. If an ingress or a web application firewall
filters methods, allow them for these paths.

## The oCIS proxy route

oCIS's own proxy can forward paths to another backend through `additional_policies` in
`proxy.yaml`, which would spare the ingress any groupware configuration. CI tries that
on every pull request: `deploy/compose/ocis-proxy.yml` puts `deploy/ocis/proxy.yaml`
into oCIS, Traefik then only knows oCIS, and the stack tests run with
`STACK_ROUTE=ocis-proxy`. The result, with oCIS 8.2.2:

**It is not usable for DAV clients, and it is not supported.** The proxy forwards
everything except REPORT. Without REPORT there is no calendar-query,
addressbook-query, multiget or sync-collection, so no CalDAV or CardDAV client gets
past the first sync.

What was found, and why, for anyone who wants to try again with a later oCIS:

- The router tries query routes first, then regex routes, then prefix routes, and
  within each kind the first match wins. There is no longest-prefix rule. Routes from
  `additional_policies` are appended after oCIS's own, so a prefix route below `/dav/`,
  `/graph/` or `/remote.php/` is never reached: oCIS's shorter prefix matches first.
- A regex route gets around that, because regex routes are tried before any prefix.
  `deploy/ocis/proxy.yaml` uses regex routes for the DAV and Graph paths and prefix
  routes where nothing in oCIS starts the same way. With it, well-known, PROPFIND,
  MKCALENDAR, PUT, GET, DELETE, the Graph paths and `/groupware/` all reach the
  service, and `/dav/spaces/`, `/dav/files/` and `/graph/v1.0/me` still reach oCIS.
- `unprotected: true` makes the proxy pass the request on without checking the
  credentials. Basic app passwords and bearer tokens arrive at the service as the
  client sent them, and the service's own 401 challenge comes back unchanged.
- REPORT is the exception. oCIS has a route of its own for `REPORT` on any path that
  contains `/dav` or `/webdav` (a regex, not anchored, for its search service). When a
  request's method has routes of its own, the router only looks at those, and the oCIS
  route is first among them. A REPORT route added through `additional_policies` lands
  behind it and is never consulted. The same applies to `/remote.php/dav/…`.
- Replacing the whole table with `policies:` instead of adding to it would let a
  REPORT route come first, at the price of copying oCIS's routing table into the
  configuration and following it through every oCIS release. That has not been tried
  and is not supported either.

`TestOCISProxyRoute` in `test/stack` keeps this record honest. It checks the paths that
do get through, and it fails the moment a REPORT reaches the service, which is when
this page needs rewriting. The CalDAV, CardDAV and Python flows skip on that route and
say so.
