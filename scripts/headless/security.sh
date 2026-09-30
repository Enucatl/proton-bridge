#!/usr/bin/env bash
set -euo pipefail
. ./scripts/headless/environment.sh
mode=${1:-all}
sbom=${2:-}
case "$mode" in
    source) ;;
    artifacts|all)
        test -x headless-dist/proton-bridge-headless
        if test -z "$sbom" || ! test -f "$sbom"; then
            echo 'Supply the release SBOM path as the second argument' >&2
            exit 1
        fi
        sbom=$(realpath "$sbom")
        command -v strings >/dev/null
        ;;
    *) echo 'Usage: scripts/headless/security.sh source | artifacts SBOM | all SBOM' >&2; exit 1;;
esac
umask 077
reports="$PWD/headless-dist/security"
mkdir -p "$reports/cache"
chmod 700 "$reports"
printf '{}\n' > "$reports/empty.yaml"
scanner=(docker run --rm --platform linux/amd64 --userns=host --user "$(id -u):$(id -g)"
    --read-only --cap-drop ALL --security-opt no-new-privileges --tmpfs /tmp --workdir /
    -e HOME=/tmp -v "$reports:/reports" -v "$reports/cache:/cache"
    -v "$PWD:/src:ro")
common=(--config /reports/empty.yaml --cache-dir /cache --timeout 15m
    --disable-telemetry --skip-version-check --ignorefile /dev/null
    --severity 'HIGH,CRITICAL' --exit-code 42 --format json)
failed=0
scan() {
    local name=$1 status=0
    shift
    rm -f "$reports/$name.json" "$reports/$name.sarif"
    "${scanner[@]}" "$TRIVY_IMAGE" "$@" "${common[@]}" --output "/reports/$name.json" || status=$?
    if test "$status" -ne 0; then
        failed=1
        if test "$status" -eq 42; then
            echo "Trivy $name: HIGH/CRITICAL findings failed the gate" >&2
        else
            echo "Trivy $name: scanner failed (exit $status)" >&2
        fi
    fi
    if test -f "$reports/$name.json"; then
        "${scanner[@]}" "$TRIVY_IMAGE" convert --config /reports/empty.yaml --format sarif \
            --output "/reports/$name.sarif" "/reports/$name.json" || failed=1
    else
        failed=1
    fi
}
if test "$mode" != artifacts; then
    scan source fs --scanners misconfig,secret --secret-config /reports/empty.yaml \
        --skip-dirs /src/.git,/src/headless-dist /src
fi
if test "$mode" != source; then
    sha256sum headless-dist/proton-bridge-headless > "$reports/scanned-binary.sha256"
    # Secret scanning reads plaintext; expose printable executable strings too.
    LC_ALL=C strings -a headless-dist/proton-bridge-headless > "$reports/binary-strings.txt"
    trap 'rm -f "$reports/binary-strings.txt"' EXIT
    scanner+=(-v "$PWD/headless-dist/proton-bridge-headless:/binary/proton-bridge-headless:ro"
        -v "$reports/binary-strings.txt:/binary/strings.txt:ro" -v "$sbom:/artifact/release.sbom.json:ro")
    scan binary rootfs --scanners vuln,secret --secret-config /reports/empty.yaml --ignore-unfixed /binary
    scan sbom sbom --scanners vuln --ignore-unfixed /artifact/release.sbom.json
    sha256sum --strict -c "$reports/scanned-binary.sha256" || failed=1
fi
{ printf 'Image: %s\n' "$TRIVY_IMAGE"; "${scanner[@]}" "$TRIVY_IMAGE" --cache-dir /cache --version; } \
    > "$reports/scanner-version.txt" || failed=1
echo "Trivy reports: $reports (gate status $failed)"
exit "$failed"
