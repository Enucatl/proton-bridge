#!/usr/bin/env bash
# Verify scanner failures cannot become successful gates or skip later scopes.
set -euo pipefail
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/scripts/headless" "$stage/headless-dist" "$stage/bin"
cp scripts/headless/{security.sh,environment.sh} "$stage/scripts/headless/"
cp /bin/true "$stage/headless-dist/proton-bridge-headless"
printf '{}\n' > "$stage/release.sbom.json"
cat > "$stage/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if test "${*: -1}" = --version; then echo 'Version: 0.74.0'; exit 0; fi
printf '%s ' "$@" >> "$MOCK_LOG"
printf '\n' >> "$MOCK_LOG"
report_dir= command= output= previous=
for argument in "$@"; do
    if test "$previous" = --output; then output=$argument; fi
    case "$argument" in
        *:/reports) report_dir=${argument%:/reports};;
        fs|rootfs|sbom|convert) command=$argument;;
    esac
    previous=$argument
done
if test "$command" = "$MOCK_FAIL_SCOPE"; then exit "$MOCK_STATUS"; fi
printf '{"Results":[]}\n' > "$report_dir/${output#/reports/}"
EOF
chmod +x "$stage/bin/docker"
export PATH="$stage/bin:$PATH" MOCK_LOG="$stage/docker.log"
cd "$stage"
for status in 42 23; do
    export MOCK_FAIL_SCOPE=fs MOCK_STATUS=$status
    : > "$MOCK_LOG"
    if scripts/headless/security.sh all release.sbom.json; then
        echo 'A failed source scan must fail the complete gate' >&2
        exit 1
    fi
    grep -F ' rootfs ' "$MOCK_LOG" >/dev/null
    grep -F ' sbom ' "$MOCK_LOG" >/dev/null
    grep -F -- '--severity HIGH,CRITICAL --exit-code 42' "$MOCK_LOG" >/dev/null
    grep -F 'aquasec/trivy:0.74.0@sha256:' "$MOCK_LOG" >/dev/null
    test ! -e headless-dist/security/binary-strings.txt
done
export MOCK_FAIL_SCOPE=none MOCK_STATUS=0
scripts/headless/security.sh all release.sbom.json
for scope in source binary sbom; do
    test -s "headless-dist/security/$scope.json"
    test -s "headless-dist/security/$scope.sarif"
done
: > "$MOCK_LOG"
if scripts/headless/security.sh artifacts missing.json; then exit 1; fi
test ! -s "$MOCK_LOG"
echo 'Trivy orchestration checks passed'
