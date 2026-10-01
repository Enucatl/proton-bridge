#!/usr/bin/env bash
set -euo pipefail
umask 077
reports="$PWD/headless-dist/security"
mkdir -p "$reports/cache"
chmod 700 "$reports"
printf '{}\n' > "$reports/empty.yaml"
exec docker run --rm --platform linux/amd64 --userns=host --user "$(id -u):$(id -g)" \
    --read-only --cap-drop ALL --security-opt no-new-privileges --tmpfs /tmp --workdir / \
    -e HOME=/tmp -v "$reports:/reports" -v "$reports/cache:/cache" -v "$PWD:/src:ro" \
    aquasec/trivy:0.74.0@sha256:ee940acbf1f58ebadb42d01434ce4609530bf1b52536afbd1eee66cd7123c5c9 fs --scanners misconfig,secret \
    --config /reports/empty.yaml --secret-config /reports/empty.yaml --ignorefile /dev/null \
    --cache-dir /cache --timeout 15m --disable-telemetry --skip-version-check \
    --skip-dirs /src/.git,/src/headless-dist --severity HIGH,CRITICAL --exit-code 42 \
    --format table /src
