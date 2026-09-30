#!/usr/bin/env bash
set -euo pipefail
source ./scripts/headless/environment.sh
work=$(mktemp -d)
containers=()
cleanup() {
    for name in "${containers[@]}"; do docker rm -f "$name" >/dev/null 2>&1 || true; done
    docker run --rm --userns=host --user 0:0 -v "$work:/work" "$BUILDER_IMAGE" chown -R "$(id -u):$(id -g)" /work >/dev/null 2>&1 || true
    rm -rf "$work"
}
trap cleanup EXIT
wait_ready() {
    local name=$1
    for ((attempt=0; attempt<30; attempt++)); do
        if docker exec "$name" /proton-bridge-headless --healthcheck >/dev/null 2>&1; then return; fi
        if [[ $(docker inspect -f '{{.State.Running}}' "$name") != true ]]; then break; fi
        sleep 1
    done
    docker logs "$name"
    return 1
}
chmod 755 "$work"
mkdir "$work/certs"
openssl req -x509 -newkey rsa:2048 -nodes -days 2 \
    -subj /CN=localhost -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1' \
    -keyout "$work/certs/key.pem" -out "$work/certs/cert.pem" >/dev/null 2>&1
chmod 644 "$work/certs/"*.pem
for target in distroless scratch; do
    image="proton-bridge-headless-smoke:$target"
    docker build --platform linux/amd64 --build-arg "DISTROLESS_IMAGE=$DISTROLESS_IMAGE" \
        --target "$target" -f scripts/headless/Dockerfile.smoke -t "$image" headless-dist
    state="$work/$target"
    mkdir "$state"
    chmod 700 "$state"
    # Numeric nonroot identity stays explicit even when a base supplies another UID.
    docker run --rm --userns=host --user 0:0 -v "$state:/state" "$BUILDER_IMAGE" chown 1000:1000 /state
    options=(--userns=host --read-only --cap-drop ALL --security-opt no-new-privileges --user 1000:1000
        --tmpfs '/tmp:rw,noexec,nosuid,uid=1000,gid=1000,mode=700'
        -v "$state:/data" -v "$work/certs:/protonmail/certs:ro")
    docker run --rm "${options[@]}" --entrypoint /runtime-probe "$image" runtime
    docker run --rm "${options[@]}" "$image" --version
    name="headless-smoke-${target}-$$"
    containers+=("$name")
    docker run -d --name "$name" "${options[@]}" "$image" --noninteractive >/dev/null
    wait_ready "$name"
    docker exec "$name" /runtime-probe
    # A second writer must fail while the service holds the state lock.
    if docker exec "$name" /proton-bridge-headless --noninteractive; then
        echo 'Concurrent state writer unexpectedly started' >&2; exit 1
    fi
    docker stop -t 20 "$name" >/dev/null
    test "$(docker inspect -f '{{.State.ExitCode}}' "$name")" = 0
    if docker run --rm "${options[@]}" "$image" --healthcheck --tls-server-name localhost; then
        echo 'Healthcheck unexpectedly passed without service' >&2; exit 1
    fi
    # Exercise the account-free interactive CLI before distributing this binary.
    printf 'help\nexit\n' | docker run --rm -i "${options[@]}" "$image" --cli
    docker rm "$name" >/dev/null
    # Default self-signed identity persists in the vault and needs no custom mount.
    fallback_state="$work/fallback-$target"
    mkdir -m 700 "$fallback_state"
    docker run --rm --userns=host --user 0:0 -v "$fallback_state:/state" "$BUILDER_IMAGE" chown 1000:1000 /state
    fallback_options=(--userns=host --read-only --cap-drop ALL --security-opt no-new-privileges --user 1000:1000
        --tmpfs '/tmp:rw,noexec,nosuid,uid=1000,gid=1000,mode=700' -v "$fallback_state:/data")
    name="headless-fallback-${target}-$$"
    containers+=("$name")
    docker run -d --name "$name" "${fallback_options[@]}" "$image" --noninteractive >/dev/null
    wait_ready "$name"
    docker exec "$name" /proton-bridge-headless --healthcheck --tls-key /missing/not-needed.pem
    docker exec "$name" /runtime-probe fallback
    docker cp "$name:/data/tls-cert.pem" "$work/first-$target.pem"
    docker stop -t 20 "$name" >/dev/null
    test "$(docker inspect -f '{{.State.ExitCode}}' "$name")" = 0
    docker start "$name" >/dev/null
    wait_ready "$name"
    docker cp "$name:/data/tls-cert.pem" "$work/restarted-$target.pem"
    cmp "$work/first-$target.pem" "$work/restarted-$target.pem"
    docker stop -t 20 "$name" >/dev/null
    test "$(docker inspect -f '{{.State.ExitCode}}' "$name")" = 0
    docker rm "$name" >/dev/null
    if docker run --rm "${fallback_options[@]}" "$image" --noninteractive --tls-cert /missing/cert.pem --tls-key /missing/key.pem; then
        echo 'Explicit missing certificate unexpectedly used fallback' >&2; exit 1
    fi
    printf 'invalid PEM\n' > "$work/certs/invalid.pem"
    if docker run --rm "${fallback_options[@]}" -v "$work/certs:/protonmail/certs:ro" "$image" \
        --noninteractive --tls-cert /protonmail/certs/invalid.pem --tls-key /protonmail/certs/key.pem; then
        echo 'Explicit malformed certificate unexpectedly used fallback' >&2; exit 1
    fi
    echo "$target: DNS, outbound TLS, SQLite, CLI, local TLS, healthcheck, locking, SIGTERM passed"
done
sha256sum headless-dist/proton-bridge-headless > headless-dist/smoke-tested-binary.sha256
