#!/bin/sh
set -eu
docker build --platform linux/amd64 --target builder \
    -t proton-bridge-headless-builder -f Dockerfile .
mkdir -p headless-dist
docker run --rm --userns=host --user 0:0 \
    -v proton-bridge-headless-gomod:/go/pkg/mod \
    -v proton-bridge-headless-gosumdb:/go/pkg/sumdb \
    -v proton-bridge-headless-gocache:/go-cache \
    proton-bridge-headless-builder chown -R "$(id -u):$(id -g)" /go/pkg/mod /go/pkg/sumdb /go-cache
docker run --rm --userns=host --user "$(id -u):$(id -g)" --platform linux/amd64 \
    -e HOME=/tmp -e GOCACHE=/go-cache \
    -e HEADLESS_VERSION -e REVISION -e SOURCE_DATE_EPOCH \
    -v "$PWD:/src" -v proton-bridge-headless-gomod:/go/pkg/mod \
    -v proton-bridge-headless-gosumdb:/go/pkg/sumdb \
    -v proton-bridge-headless-gocache:/go-cache \
    proton-bridge-headless-builder "./utils/headless/${1:-build}.sh"
