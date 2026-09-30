#!/usr/bin/env bash
set -euo pipefail
. ./scripts/headless/environment.sh
: "${HEADLESS_VERSION:=v${UPSTREAM_VERSION}-dev}"
: "${SOURCE_DATE_EPOCH:=$(git log -1 --format=%ct)}"
case "$HEADLESS_VERSION" in *[!a-zA-Z0-9._-]*|'') echo 'Invalid release version' >&2; exit 1;; esac
export GOFLAGS='-mod=readonly -tags=container,netgo,osusergo,sqlite_omit_load_extension'
if test -n "$(git status --porcelain --no-branch --untracked-files=normal)"; then
    echo 'Packaging requires committed source; a dirty tree would not match git archive HEAD' >&2
    exit 1
fi
binary=headless-dist/proton-bridge-headless
test -x "$binary"
sha256sum --strict -c headless-dist/smoke-tested-binary.sha256
"$binary" --version | grep -F "$HEADLESS_VERSION (upstream $UPSTREAM_VERSION, revision $(git rev-parse HEAD))" >/dev/null
go version -m "$binary" > headless-dist/current-build-info.txt
cmp headless-dist/build-info.txt headless-dist/current-build-info.txt
release="proton-bridge-headless-$HEADLESS_VERSION"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/$release/licenses" headless-dist/release
cp "$binary" LICENSE COPYING_NOTES.md HEADLESS.md "$stage/$release/"
cp scripts/headless/MUSL-COPYRIGHT "$stage/$release/licenses/musl-COPYRIGHT"
cp scripts/headless/GCC-RUNTIME-EXCEPTION "$stage/$release/licenses/GCC-RUNTIME-EXCEPTION"
cp "$(go env GOROOT)/LICENSE" "$stage/$release/licenses/Go-LICENSE"
# Preserve license/notice texts for the modules actually used by the binary.
go list -deps -json ./cmd/proton-bridge-headless |
    jq -r 'select(.Module and (.Module.Main != true)) | .Module | if .Replace then .Replace else . end | [.Path,.Dir] | @tsv' |
    sort -u > "$stage/modules"
while IFS="$(printf '\t')" read -r module directory; do
    destination="$stage/$release/licenses/$module"
    mkdir -p "$destination"
    (cd "$directory" && find . -maxdepth 2 -type f \( -iname 'license*' -o -iname 'copying*' -o -iname 'notice*' -o -iname 'copyright*' \) \
        -exec cp --parents -t "$destination/" '{}' +)
done < "$stage/modules"
cp headless-dist/build-info.txt headless-dist/build-packages.txt headless-dist/dependencies.txt "$stage/$release/"
tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
    -czf "headless-dist/release/$release-linux-amd64.tar.gz" -C "$stage" "$release"
# git archive records the exact committed fork source, with dependency locks.
git archive --format=tar --prefix="$release-source/" HEAD |
    gzip -n > "headless-dist/release/$release-source.tar.gz"
cyclonedx-gomod app -json -licenses -main cmd/proton-bridge-headless \
    -output "headless-dist/release/$release.sbom.json" .
# CGO's embedded SQLite and static native runtime are outside Go module metadata.
sqlite_directory=$(go list -m -f '{{.Dir}}' github.com/mattn/go-sqlite3)
sqlite_version=$(sed -n 's/^#define SQLITE_VERSION[[:space:]]*"\([^"]*\)".*/\1/p' "$sqlite_directory/sqlite3-binding.h")
test -n "$sqlite_version"
jq --arg musl "$(sed -n 's/^musl=//p' scripts/headless/apk.lock)" \
    --arg gcc "$(sed -n 's/^libgcc-static=//p' scripts/headless/apk.lock)" \
    --arg sqlite "$sqlite_version" '
    [
      {type:"library",name:"musl",version:$musl,"bom-ref":("native:musl:"+$musl)},
      {type:"library",name:"GCC runtime",version:$gcc,"bom-ref":("native:libgcc:"+$gcc)},
      {type:"library",name:"SQLite",version:$sqlite,"bom-ref":("native:sqlite:"+$sqlite)}
    ] as $native |
    .metadata.component["bom-ref"] as $root |
    .components += $native |
    .dependencies |= map(if .ref == $root then .dependsOn += ($native | map(.["bom-ref"])) else . end)
    ' "headless-dist/release/$release.sbom.json" > "$stage/native.sbom.json"
mv "$stage/native.sbom.json" "headless-dist/release/$release.sbom.json"
jq -n --arg version "$HEADLESS_VERSION" --arg upstream "$UPSTREAM_VERSION" \
    --arg revision "$(git rev-parse HEAD)" --arg builder "$GO_IMAGE" \
    --arg source_date_epoch "$SOURCE_DATE_EPOCH" \
    --arg runtime "$DISTROLESS_IMAGE" --arg binary_sha256 "$(sha256sum "$binary" | cut -d ' ' -f1)" \
    '{version:$version,upstream:$upstream,revision:$revision,source_date_epoch:$source_date_epoch,builder:$builder,runtime:$runtime,binary_sha256:$binary_sha256,runtime_shared_libraries:[],cgo:true}' \
    > headless-dist/release/build-metadata.json
cat > headless-dist/release/RELEASE_NOTES.md <<EOF
Based on Proton Mail Bridge $UPSTREAM_VERSION; source revision $(git rev-parse HEAD).
Linux amd64 static CGO/musl binary, bundled SQLite; no runtime shared libraries.
Headless startup, private file vault key, native implicit TLS IMAP/SMTP,
disabled desktop integrations, self-updates and automated telemetry, crash and
TLS diagnostic uploads; user-requested bug reports remain available.
Clients must use implicit TLS on container ports 1143/1025 and verify the
certificate hostname. See HEADLESS.md inside the release for configuration.
Optional custom certificates retain the stored self-signed fallback when both
default files are absent; explicit invalid/missing custom files stop startup.
Stop the old service and snapshot all state before migration. Never run two
instances against one state directory. Restore the full snapshot for rollback
when state migrations/token rotation prevent image-only rollback; possible
reauthentication. Live account/client acceptance must precede publication.
The GitHub release uses the existing upstream tag; its automatic source links
refer to upstream. Use the attached fork source archive and build metadata to
reproduce this binary from the source revision above.
The separate Docker repository owns runtime packaging and deployment.
EOF
(cd headless-dist/release && sha256sum ./*.tar.gz ./*.json RELEASE_NOTES.md > SHA256SUMS)
