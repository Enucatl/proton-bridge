#!/bin/sh
set -eu
UPSTREAM_VERSION=$(sed -n 's/^BRIDGE_APP_VERSION?=\([0-9.]*\)+git$/\1/p' Makefile)
: "${REVISION:=$(git rev-parse HEAD)}"
: "${HEADLESS_VERSION:=v${UPSTREAM_VERSION}-dev-$(printf '%.12s' "$REVISION")}"
: "${SOURCE_DATE_EPOCH:=$(git log -1 --format=%ct 2>/dev/null || date +%s)}"
export GOOS=linux GOARCH=amd64 CGO_ENABLED=1 GOTOOLCHAIN=local
export GOFLAGS='-mod=readonly -tags=container,netgo,osusergo,sqlite_omit_load_extension'
mkdir -p headless-dist
go list -deps ./cmd/proton-bridge-headless > headless-dist/dependencies.txt
if grep -E '/(frontend/grpc|frontend/bridge-gui|fido|focus/proto|autostart|bridge/mocks)(/|$)|go-libfido2|go-ctap|go-keychain|godbus|docker-credential-helpers/(pass|secretservice)|therecipe/qt|0xAX/notificator|google.golang.org/grpc|go.uber.org/mock' headless-dist/dependencies.txt; then
    echo 'Forbidden desktop or test dependency in headless binary' >&2
    exit 1
fi
prefix=github.com/ProtonMail/proton-bridge/v3/internal/constants
build_time=$(date -u -d "@$SOURCE_DATE_EPOCH" +%FT%TZ)
go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X $prefix.Version=$UPSTREAM_VERSION -X $prefix.DownstreamVersion=$HEADLESS_VERSION -X $prefix.Revision=$REVISION -X $prefix.Tag=$HEADLESS_VERSION -X $prefix.BuildTime=$build_time -X $prefix.BuildEnv=headless -X '$prefix.FullAppName=Proton Mail Bridge Headless'" \
    -o headless-dist/proton-bridge-headless ./cmd/proton-bridge-headless
readelf -h headless-dist/proton-bridge-headless | grep 'Advanced Micro Devices X86-64'
go version -m headless-dist/proton-bridge-headless > headless-dist/build-info.txt
dpkg-query -W > headless-dist/build-packages.txt
headless-dist/proton-bridge-headless --version
