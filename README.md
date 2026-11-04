# ocis-groupware

Calendar and contacts for ownCloud Infinite Scale — CalDAV/CardDAV, Graph-shaped API, Vue UI

## Development

Requires Go 1.26.10.

```sh
go build ./...
go test ./...
go run ./cmd/ocis-groupware
```

Config is env vars (`GROUPWARE_HTTP_ADDR`, `GROUPWARE_LOG_LEVEL`, `GROUPWARE_OIDC_ISSUER`,
`GROUPWARE_DB_DSN`) and/or a YAML file passed via `--config groupware.yaml`; env wins
over file, file wins over defaults.

`/healthz` and `/metrics` are served unauthenticated on `GROUPWARE_HTTP_ADDR`
(default `:8080`).

## Docker

```sh
docker build -t ocis-groupware:dev .
docker run --rm -p 8080:8080 ocis-groupware:dev
```

## License

[Apache License 2.0](LICENSE).
