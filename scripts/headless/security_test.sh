#!/usr/bin/env bash
# Source findings and scanner failures must block the push.
set -euo pipefail
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/scripts/headless" "$stage/bin"
cp scripts/headless/security.sh "$stage/scripts/headless/"
cat > "$stage/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$MOCK_LOG"
test "$1" = run
exit "$MOCK_SCAN_STATUS"
MOCK
chmod +x "$stage/bin/docker"
export PATH="$stage/bin:$PATH" MOCK_LOG="$stage/docker.log"
cd "$stage"
for status in 0 42 23; do
    export MOCK_SCAN_STATUS=$status
    : > "$MOCK_LOG"
    actual=0
    scripts/headless/security.sh || actual=$?
    test "$actual" -eq "$status"
    grep -F 'aquasec/trivy:0.74.0@sha256:ee940acbf1f58ebadb42d01434ce4609530bf1b52536afbd1eee66cd7123c5c9' "$MOCK_LOG" >/dev/null
    grep -F ' fs --scanners misconfig,secret ' "$MOCK_LOG" >/dev/null
    grep -F -- '--severity MEDIUM,HIGH,CRITICAL --exit-code 42' "$MOCK_LOG" >/dev/null
    grep -F -- '--ignorefile /dev/null' "$MOCK_LOG" >/dev/null
done
echo 'Source security gate checks passed'
