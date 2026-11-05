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

Only these go to the service, plus the classic paths below when they are switched on.
`/dav/spaces/`, `/dav/files/`, `/remote.php/`, the rest of `/graph/` and everything else
stay with oCIS. The matches are on the path only; the
service does not need the ingress to rewrite anything or add headers.

`/healthz` and `/metrics` are for the monitoring side. `/metrics` is not on the service's
public port at all: it is served on the debug address (`GROUPWARE_DEBUG_ADDR`, default
`127.0.0.1:9180`, port 9180 in the container image and the Helm chart), which has no
authentication. Scrape that address from inside the cluster or host and do not route it.
`/healthz` is answered on both ports. The development stack routes it through Traefik so
the stack tests can wait for the service, and the snippets below leave it out. Reach it
on the service's own address.

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

On Kubernetes, the Helm chart in `deploy/helm/ocis-groupware` creates an Ingress with the
same paths. An Ingress has no regular expressions, so it lists the Graph paths one by one.

## The classic paths

A client that was set up against a classic server keeps the addresses it learned there.
Classic servers serve DAV below `/remote.php/dav`, with principals at
`principals/users/{user}/`, address books at `addressbooks/users/{user}/{book}/` and
calendars at `calendars/{user}/{calendar}/`. With `GROUPWARE_COMPAT_PATHS=true` the
service serves that layout as well, under the same prefix, from the same data and behind
the same sign-in as `/dav`: app passwords, imported classic tokens and bearer tokens, and
the same view of shared collections. `/dav` itself is unchanged, and the classic paths
are off by default. They only work for a user whose classic login name is the same as the
oCIS login name, because the user's name is part of every address. Other users add their
devices again.

[migration.md](migration.md) puts this in the context of a move from a classic server.
The ingress has to send these paths to the service, and only these:

| Path | What it is |
|---|---|
| `/remote.php/dav/principals/` | Principals, which clients ask for their home sets |
| `/remote.php/dav/addressbooks/` | CardDAV |
| `/remote.php/dav/calendars/` | CalDAV |

`/remote.php/dav/files/`, `/remote.php/dav/spaces/` and the rest of `/remote.php/` stay
with oCIS, which answers those and has nothing at the three paths above, so routing them
takes nothing away from it. The bare `/remote.php/dav/` is not in the list: oCIS answers
it for its file clients. Clients with a stored address do not need it.

The compose file `deploy/compose/compat-paths.yml` turns the option on and adds the
Traefik route, and CI runs the stack tests against it:

```sh
docker compose -f docker-compose.yml -f deploy/compose/compat-paths.yml up --build
```

The same routes for the other ingresses. Traefik, next to the existing router:

```yaml
    ocis-groupware-compat:
      rule: >-
        Host(`ocis.example.com`) && (
        PathPrefix(`/remote.php/dav/principals/`) ||
        PathPrefix(`/remote.php/dav/addressbooks/`) ||
        PathPrefix(`/remote.php/dav/calendars/`))
      entryPoints: [websecure]
      service: ocis-groupware
      priority: 100
```

nginx, in the oCIS server block with the other locations:

```nginx
    location ^~ /remote.php/dav/principals/ {
        proxy_pass http://ocis_groupware;
    }
    location ^~ /remote.php/dav/addressbooks/ {
        proxy_pass http://ocis_groupware;
    }
    location ^~ /remote.php/dav/calendars/ {
        proxy_pass http://ocis_groupware;
    }
```

Caddy, next to the `@groupware` matcher, handled by the same `handle` block (or add the
paths to that matcher):

```caddy
    @groupware_compat path /remote.php/dav/principals/* /remote.php/dav/addressbooks/* /remote.php/dav/calendars/*

    handle @groupware_compat {
        reverse_proxy ocis-groupware:8080
    }
```

These paths are supported through an ingress only. The oCIS proxy route below cannot
carry them for the reason given there: oCIS answers every REPORT below `/remote.php/dav`
with its search service, so no query or sync request would reach the service. Nothing in
the service or the compose files adds such a route.

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
