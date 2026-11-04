# ocis-groupware

Calendar and contacts for ownCloud Infinite Scale — CalDAV/CardDAV, Graph-shaped API, Vue UI

## Development

Requires Go 1.26.10 and [go-task](https://taskfile.dev).

```sh
task build
task test
task run
```

Run `task --list-all` for the full list, or `task check` to run fmt/vet/lint/test
together (what CI runs).

Config is env vars (`GROUPWARE_HTTP_ADDR`, `GROUPWARE_LOG_LEVEL`, `GROUPWARE_OIDC_ISSUER`,
`GROUPWARE_DB_DSN`) and/or a YAML file passed via `--config groupware.yaml`; env wins
over file, file wins over defaults.

`/healthz` and `/metrics` are served unauthenticated on `GROUPWARE_HTTP_ADDR`
(default `:8080`).

## Docker

```sh
task docker:build
task docker:run
```

## Local stack

`docker-compose.yml` runs oCIS and ocis-groupware behind one Traefik on the same
domain, which ocis-groupware requires. ocis-groupware's DAV, Graph, `/web/` and ops paths route
to ocis-groupware; everything else goes to oCIS.

```sh
task stack:up
```

Then open <https://ocis.localhost> (self-signed certificate, `admin` / `admin`).
Set `OCIS_DOMAIN`, `OCIS_VERSION` or `IDM_ADMIN_PASSWORD` in the environment
or a `.env` file to override the defaults. This stack is for development only.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Please also read the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Security

See [SECURITY.md](SECURITY.md) for how to report vulnerabilities.

## Support

See [SUPPORT.md](SUPPORT.md) for where to get help.

## License

[Apache License 2.0](LICENSE).
