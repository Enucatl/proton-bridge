#!/usr/bin/env bash
set -euo pipefail
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir "$stage/bin"
cat > "$stage/bin/go" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
test "$GOTOOLCHAIN" = local
test "$GOOS/$GOARCH/$CGO_ENABLED" = linux/amd64/1
test "$GOFLAGS" = '-tags=container,netgo,osusergo,sqlite_omit_load_extension'
printf '%s\n' "$*" >> "$MOCK_LOG"
if [[ $* == 'list -deps -test -f '* ]]; then
    test "${*: -2}" = './cmd/proton-bridge-headless ./internal/bridge'
    if [[ ${MOCK_LIST_STATUS:-0} != 0 ]]; then exit "$MOCK_LIST_STATUS"; fi
fi
case "$*" in
    'env GOVERSION') echo go1.27.1;;
    'mod edit -toolchain=go1.27.1') ;;
    'list -deps -f '*)
        printf '%s\n' ./cmd/proton-bridge-headless ./internal/bridge;;
    'list -deps -test -f {{if and .Module (not .Module.Main)}}'*)
        printf '%s\n' example.com/runtime-dependency@upgrade \
            example.com/test-dependency@upgrade example.com/runtime-dependency@upgrade;;
    'list -deps -test -f '*)
        # Repeated modules and a replacement used only by engine tests.
        printf '%s\n' \
            'example.com/test-dependency github.com/ProtonMail/test-fork v0.0.0-pinned' \
            'example.com/test-dependency github.com/ProtonMail/test-fork v0.0.0-pinned';;
    'list -m -f {{.Version}} {{.Time.Unix}} github.com/ProtonMail/test-fork@HEAD')
        exit_status=${MOCK_QUERY_STATUS:-0}
        if (( exit_status != 0 )); then exit "$exit_status"; fi
        echo "v0.0.0-candidate $MOCK_HEAD_TIME";;
    'list -m -f {{.Time.Unix}} github.com/ProtonMail/test-fork@v0.0.0-pinned') echo 100;;
    'mod edit -replace=example.com/test-dependency=github.com/ProtonMail/test-fork@v0.0.0-candidate') ;;
    'get example.com/runtime-dependency@upgrade example.com/test-dependency@upgrade')
        touch "$MOCK_LOG.upgraded";;
    'list -mod=mod -deps -test ./cmd/proton-bridge-headless ./internal/bridge')
        test -f "$MOCK_LOG.upgraded"
        exit "${MOCK_RESOLVE_STATUS:-0}";;
    *) echo "Unexpected go command: $*" >&2; exit 1;;
esac
MOCK
chmod +x "$stage/bin/go"
export PATH="$stage/bin:$PATH" MOCK_LOG="$stage/go.log" MOCK_HEAD_TIME=200
script="$(pwd)/utils/headless/latest-deps.sh"
cd "$stage"

"$script"
test "$(grep -c '^mod edit -replace=' "$MOCK_LOG")" -eq 1
grep -Fx 'get example.com/runtime-dependency@upgrade example.com/test-dependency@upgrade' "$MOCK_LOG" >/dev/null
test "$(tail -n 1 "$MOCK_LOG")" = 'list -mod=mod -deps -test ./cmd/proton-bridge-headless ./internal/bridge'

for MOCK_HEAD_TIME in 50 100; do
    export MOCK_HEAD_TIME
    : > "$MOCK_LOG"
    "$script"
    if grep -q '^mod edit -replace=' "$MOCK_LOG"; then exit 1; fi
done

# Neither a failed query nor a failed package list may silently skip upgrades.
for failure in MOCK_QUERY_STATUS MOCK_LIST_STATUS; do
    export "$failure=17"
    : > "$MOCK_LOG"
    actual=0
    "$script" || actual=$?
    test "$actual" -eq 17
    if grep -q '^get ' "$MOCK_LOG"; then exit 1; fi
    unset "$failure"
done

# A failed resolution must fail the upgrade too.
export MOCK_RESOLVE_STATUS=17
actual=0
"$script" || actual=$?
test "$actual" -eq 17
echo 'Headless dependency upgrade checks passed'
