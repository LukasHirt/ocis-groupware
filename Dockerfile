# syntax=docker/dockerfile:1

FROM golang:1.27.3-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9 AS build
WORKDIR /src
ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN go build -trimpath -ldflags="-s -w" -o /out/ocis-groupware ./cmd/ocis-groupware
# Distroless has no shell, so the data directory is created here and copied over with its owner.
RUN mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/ocis-groupware /ocis-groupware
COPY --from=build --chown=65532:65532 /out/data /data
ENV GROUPWARE_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/ocis-groupware"]
