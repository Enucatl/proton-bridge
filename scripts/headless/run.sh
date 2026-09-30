#!/bin/sh
set -eu
. ./scripts/headless/environment.sh
docker build --platform linux/amd64 --build-arg "GO_IMAGE=$GO_IMAGE" \
    -t "$BUILDER_IMAGE" -f scripts/headless/Dockerfile.builder .
mkdir -p headless-dist
docker run --rm --userns=host --user 0:0 \
    -v proton-bridge-headless-gomod:/go/pkg/mod \
    -v proton-bridge-headless-gocache:/go-cache \
    "$BUILDER_IMAGE" chown -R "$(id -u):$(id -g)" /go/pkg/mod /go-cache
docker run --rm --userns=host --user "$(id -u):$(id -g)" --platform linux/amd64 \
    -e HOME=/tmp -e GOCACHE=/go-cache \
    -e HEADLESS_VERSION -e REVISION -e SOURCE_DATE_EPOCH \
    -v "$PWD:/src" -v proton-bridge-headless-gomod:/go/pkg/mod \
    -v proton-bridge-headless-gocache:/go-cache \
    "$BUILDER_IMAGE" "./scripts/headless/${1:-build}.sh"
