#!/usr/bin/env bash
# Source findings, scanner errors and build failures must block the push.
set -euo pipefail
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/scripts/headless" "$stage/bin"
cp scripts/headless/{security.sh,Dockerfile.scanner} "$stage/scripts/headless/"
cat > "$stage/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$MOCK_LOG"
case "$1" in
    build) exit "$MOCK_BUILD_STATUS";;
    run) exit "$MOCK_SCAN_STATUS";;
    *) exit 1;;
esac
MOCK
chmod +x "$stage/bin/docker"
export PATH="$stage/bin:$PATH" MOCK_LOG="$stage/docker.log" MOCK_BUILD_STATUS=0
cd "$stage"
for status in 0 42 23; do
    export MOCK_SCAN_STATUS=$status
    : > "$MOCK_LOG"
    actual=0
    scripts/headless/security.sh source || actual=$?
    test "$actual" -eq "$status"
    grep -F 'build --platform linux/amd64 -f scripts/headless/Dockerfile.scanner' "$MOCK_LOG" >/dev/null
    grep -F ' fs --scanners misconfig,secret ' "$MOCK_LOG" >/dev/null
    grep -F -- '--severity HIGH,CRITICAL --exit-code 42' "$MOCK_LOG" >/dev/null
    grep -F -- '--ignorefile /dev/null' "$MOCK_LOG" >/dev/null
done
grep -F 'aquasec/trivy:0.74.0@sha256:' scripts/headless/Dockerfile.scanner >/dev/null
export MOCK_BUILD_STATUS=17 MOCK_SCAN_STATUS=0
: > "$MOCK_LOG"
actual=0
scripts/headless/security.sh source || actual=$?
test "$actual" -eq 17
test "$(wc -l < "$MOCK_LOG")" -eq 1
: > "$MOCK_LOG"
if scripts/headless/security.sh artifacts; then exit 1; fi
test ! -s "$MOCK_LOG"
echo 'Source security gate checks passed'
