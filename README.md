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

## License

[Apache License 2.0](LICENSE).
