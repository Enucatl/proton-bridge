#!/usr/bin/env bash
set -euo pipefail
if test "${1:-source}" != source || test "$#" -gt 1; then
    echo 'Usage: scripts/headless/security.sh [source]' >&2
    exit 1
fi
docker build --platform linux/amd64 -f scripts/headless/Dockerfile.scanner \
    -t proton-bridge-headless-scanner scripts/headless
umask 077
reports="$PWD/headless-dist/security"
mkdir -p "$reports/cache"
chmod 700 "$reports"
printf '{}\n' > "$reports/empty.yaml"
exec docker run --rm --platform linux/amd64 --userns=host --user "$(id -u):$(id -g)" \
    --read-only --cap-drop ALL --security-opt no-new-privileges --tmpfs /tmp --workdir / \
    -e HOME=/tmp -v "$reports:/reports" -v "$reports/cache:/cache" -v "$PWD:/src:ro" \
    proton-bridge-headless-scanner fs --scanners misconfig,secret \
    --config /reports/empty.yaml --secret-config /reports/empty.yaml --ignorefile /dev/null \
    --cache-dir /cache --timeout 15m --disable-telemetry --skip-version-check \
    --skip-dirs /src/.git,/src/headless-dist --severity HIGH,CRITICAL --exit-code 42 \
    --format table /src
