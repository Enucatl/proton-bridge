#!/bin/sh
set -eu
export GOTOOLCHAIN=local CGO_ENABLED=1 GOOS=linux GOARCH=amd64
export GOFLAGS='-mod=readonly -tags=container,netgo,osusergo,sqlite_omit_load_extension'
./utils/headless/security_test.sh
unformatted=$(gofmt -l cmd/proton-bridge-headless internal utils/headless/probe.go)
test -z "$unformatted" || { printf '%s\n' "$unformatted" >&2; exit 1; }
# Upstream engine unit/integration tests use fake Proton APIs and real mail sockets.
# The preserved desktop Godog/GUI harness depends on excluded desktop packages.
packages=$(go list -deps -f '{{if and (not .Standard) .Module}}{{if .Module.Main}}{{.ImportPath}}{{end}}{{end}}' ./cmd/proton-bridge-headless)
# Go import paths are whitespace-free; expand this list into command arguments.
# shellcheck disable=SC2086
set -- $packages
go vet "$@"
go test -count=1 -timeout=20m "$@"
go test -race -count=1 -timeout=40m ./internal/app ./internal/bridge ./internal/vault ./internal/certs ./internal/frontend/cli ./internal/services/imapsmtpserver ./internal/user ./internal/telemetry ./internal/sentry ./internal/services/observability ./internal/unleash ./internal/dialer ./cmd/proton-bridge-headless
govulncheck ./cmd/proton-bridge-headless
./utils/headless/build.sh
