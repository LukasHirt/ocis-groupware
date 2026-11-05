# Security notes

This page says what the service checks, what it cannot check, and what an operator can do
about the difference. How to report a vulnerability is in [SECURITY.md](../SECURITY.md).

## Which tokens are accepted

A bearer token has to be signed by the oCIS identity provider, unexpired, and issued to
one of the OAuth clients in `GROUPWARE_OIDC_AUDIENCE`. The default is `web`, the client of
oCIS Web. The client is read from the `aud` claim, and from `azp` for identity providers
that put a generic audience in `aud`. A token that was issued to another client of the same
identity provider is refused with `401`. This stops a token that leaked from an unrelated
application from being used here, and it limits who can create an app password to tokens of
the clients you listed.

The check does not look at the token type. In oCIS the ID token of the `web` client carries
the same audience as its access token, so refusing ID tokens would add nothing, and the
claims that tell the types apart differ between identity providers. `*` turns the client
check off and is logged at startup.

## Offboarding

The service holds no credential for oCIS. It sees a user only through that user's own
bearer token, so it is never told that someone was disabled, deleted or removed from a
group. What it records is when oCIS last confirmed the user, at each sign-in with a token.
An app password, or an imported classic token, is accepted only while that confirmation is
younger than `GROUPWARE_IDENTITY_MAX_AGE`, 30 days by default.

This gives a bound, not an immediate cutoff. After a user is disabled or deleted in oCIS,
their app passwords keep working until the lease runs out, at most that long after the
last time they opened oCIS. Their group shares are reachable over DAV for the same time. A
deleted or disabled user cannot get a token to renew the lease, so it does end. To cut
access at once, also run `ocis-groupware admin principals disable <user>`; that takes
effect immediately and is the offboarding step to put on the checklist. Users who stay
active renew their own lease, so a stolen app password of an active user is not bounded by
it. The user can revoke it, and the service logs who created it.

The lease and what it changes for existing deployments are described in the README.

## Creating an app password

Anyone who controls a user's oCIS Web session, for example script that runs on the oCIS
origin, can create an app password for that user, just as they can do anything else the
session allows. The service cannot ask for the password again, because the oCIS identity
provider does not tell it when the user last signed in. Access tokens from the built-in
provider carry no `auth_time`, and `iat` changes at every silent refresh, so "signed in
recently" cannot be read from them.

What limits the damage:

- Only tokens of the configured clients can create one.
- A password lives at most `GROUPWARE_APP_PASSWORD_MAX_LIFETIME`, 180 days by default.
  Longer lifetimes are refused, and a request without one gets this value. Passwords that
  exist keep the expiry they had.
- A user sees every app password with its creation and last-use time, and can revoke it.
- Each creation and revocation is logged at Info with the owner, the id, the name, the
  expiry, the OAuth client of the token and the client address, which is the address the
  sign-in rate limiter counts, so it follows `GROUPWARE_TRUSTED_PROXIES`. The password
  itself is never logged.

Asking for a recent sign-in before an app password is created needs the web application to
run a second authorization request, and is not part of this release.

## Storage

The service does not limit how much a user can store, in bytes or in objects. Every writer
is an authenticated user, and a user who can write to a shared collection is someone the
owner chose. Give the data directory its own volume, alert at 80 percent use, and use
`ocis-groupware admin principals disable` against a user who abuses it.
