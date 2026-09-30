#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local CGO_ENABLED=1 GOOS=linux GOARCH=amd64
export GOFLAGS='-tags=container,netgo,osusergo,sqlite_omit_load_extension'
go mod edit -toolchain="$(go env GOVERSION)"

# Match the packages checked by check.sh, including their test-only dependencies.
packages=$(go list -deps -f '{{if and (not .Standard) .Module}}{{if .Module.Main}}{{.ImportPath}}{{end}}{{end}}' ./cmd/proton-bridge-headless)
# Go import paths are whitespace-free.
# shellcheck disable=SC2086
set -- $packages
modules=$(go list -deps -test -f '{{if and .Module (not .Module.Main)}}{{.Module.Path}}@upgrade{{end}}' "$@" | sort -u)
replacements=$(go list -deps -test -f '{{if .Module}}{{if .Module.Replace}}{{.Module.Path}} {{.Module.Replace.Path}} {{.Module.Replace.Version}}{{end}}{{end}}' "$@" | sort -u)

# Fork tags may predate Proton's patches; resolve the default branch to a pin.
while read -r original replacement pinned; do
    test -n "$original" || continue
    candidate=$(go list -m -f '{{.Version}} {{.Time.Unix}}' "$replacement@HEAD")
    read -r version timestamp <<< "$candidate"
    pinned_timestamp=$(go list -m -f '{{.Time.Unix}}' "$replacement@$pinned")
    # Some pins are on Proton feature branches ahead of the default branch.
    if (( timestamp <= pinned_timestamp )); then
        echo "Keeping $replacement@$pinned: default branch is not newer"
        continue
    fi
    go mod edit "-replace=$original=$replacement@$version"
done <<< "$replacements"

# Explicit modules retain our build-tag scope; go get -u considers all tags.
# shellcheck disable=SC2086
set -- $modules
go get "$@"
