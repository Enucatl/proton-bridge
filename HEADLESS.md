# Proton Mail Bridge headless

This fork publishes a Linux amd64 container based on upstream `v3.27.1` as
`ghcr.io/enucatl/proton-bridge:v3.27.1`. Each successful push to `master`
replaces that tag with the image built from that commit. The Docker Compose
repository consumes the image; this repository owns its build, tests, and scans.

## Build and verify

Docker, Bash, OpenSSL, Git, and an amd64 host are required:

```sh
docker build --platform linux/amd64 --build-arg REVISION="$(git rev-parse HEAD)" \
    -f scripts/headless/Dockerfile.image -t protonmail-bridge-headless:local .
make headless-check
make headless-smoke
```

`scripts/headless/Dockerfile.image` pins the Go 1.26.7 Debian Trixie builder
and Distroless `base-debian13:nonroot` runtime directly by version and digest.
The builder runs `apt-get update` and `apt-get upgrade` to install available
package fixes. CI disables the baseline's build cache so each run executes the
upgrade; use `docker build --no-cache` locally to refresh those packages.

The reduced binary uses the `container,netgo,osusergo,sqlite_omit_load_extension`
build tags. CGO retains upstream's embedded SQLite and links against Debian's
libc, supplied by the runtime image. Desktop and test-only dependencies are
rejected from the compiled application graph. Runtime UID/GID remains 1000,
with private state in `/data`; the image contains no shell or build tools.
Upstream version comes from the existing Makefile. The optional `REVISION`
build argument records the source commit; CI supplies it automatically.

The project check job retains formatting, vet, upstream engine unit/integration
tests, affected-package races, and reachable Go vulnerability checks. Smoke tests
build the same Dockerfile's `smoke` target, which adds a test-only probe to the
production runtime. They check DNS, outbound TLS, SQLite writes, IPv4/IPv6 local
TLS greetings, CLI, healthcheck, exclusive state locking, SIGTERM, persistent
self-signed certificates, and invalid custom certificate rejection. Containers
run with a read-only root, no capabilities, and private writable state and
`/tmp`. Local fixtures use the host user namespace for UID-remapped Docker
compatibility. Network-dependent checks require outbound DNS/HTTPS.

[The security baseline](https://github.com/Enucatl/docker-compose-security-baseline/blob/main/.github/workflows/docker-ci.yml)
owns image building, GHCR publishing, image SBOM generation, signing, source
misconfiguration/secret scans, and image vulnerability/secret scans, with its
existing `HIGH,CRITICAL` policy and reports. The only project-specific prerequisite
is the application check/smoke job; there is no separate release archive or
binary/SBOM scan pipeline. The baseline scans images after publication, so a
failed image scan leaves the pushed tag available. It does not scan builder
stages, and its image SBOM does not separately inventory embedded SQLite.

Pull requests build and scan source without publishing. Successful pushes to
`master` publish `ghcr.io/enucatl/proton-bridge:v3.27.1`; later pushes on the
same upstream base replace that tag. Builds compile this fork's source directly.
To upgrade, merge or rebase upstream, resolve conflicts, and rerun checks.

The separate [latest-deps workflow](.github/workflows/latest-deps.yml) runs daily
at 05:22 UTC and can be started manually. In its temporary checkout it resolves
the latest stable Go Trixie builder and refreshed Debian 13 runtime to digests,
upgrades dependencies of the headless binary and all packages tested by the
headless check, and repins reachable Proton replacements to newer default branch
heads, preserving pins whose commits are newer than the default branch. It runs
the existing checks, builds a candidate image, and runs smoke tests without
publishing. Each run saves `updates.patch` and the resulting
`go.mod`, `go.sum`, and Dockerfile in a `latest-deps` artifact for 14 days,
including failed attempts. Review and apply a passing patch with
`git apply updates.patch` to adopt the tested pins. Go upgrades stay within
existing module paths; migrations such as `/v2` to `/v3` still require explicit
[import and API changes](https://go.dev/ref/mod#major-version-suffixes).

The local `.githooks/pre-push` hook keeps the source misconfiguration/secret
gate using Trivy pinned in `scripts/headless/Dockerfile.scanner`. Docker is
required; findings and scanner errors block the push. Enable it in a new clone:

```sh
git config --local core.hooksPath .githooks
```

Local binary output from `make headless-build` lives in `headless-dist`.
Live Proton account/client acceptance remains a separate deployment check.

## Operation

Execute the binary directly as PID 1:

```sh
proton-bridge-headless --noninteractive --vault-key-file /run/secrets/bridge_vault_key
proton-bridge-headless --cli --vault-key-file /run/secrets/bridge_vault_key
proton-bridge-headless --healthcheck --tls-server-name bridge.example.test
```

State defaults to `/data` (`--data-dir`). Keep the directory private and writable
by the explicitly chosen service UID/GID. Container deployments supply an existing
32-byte raw key using `--vault-key-file`, normally a read-only Compose secret at
`/run/secrets/bridge_vault_key`. See [README.md](README.md#vault-key-secret) for
generation and remapped-user ACLs. This path is read without changes or durability
operations; missing, malformed, or unsafe secret files stop startup, even on fresh
state. There is no fallback to `/data/vault.key` when this option is supplied.

Without `--vault-key-file`, standalone operation retains `/data/vault.key`, generated
only on fresh initialization with mode 0600 and a private 0700 parent. A lost or
invalid key, damaged vault or decryption failure stops startup without resetting
existing state. Never run concurrent instances against the same state. Losing the
key loses access to the encrypted vault. With an external secret, back up the key
separately from state and encrypt both backups independently. SQLite metadata and
logs are plaintext; the vault and message-content files use application encryption.

Certificates default to `/protonmail/certs/cert.pem` and
`/protonmail/certs/key.pem` (`--tls-cert`, `--tls-key`). When both optional default
files are absent, Bridge reuses its stored self-signed certificate, generating one
on fresh initialization. Clients must explicitly trust that certificate; its
default identity covers 127.0.0.1. For a custom hostname/issuer, mount a readable
PEM certificate chain and matching private key. An explicit, partially present,
or invalid custom pair stops startup. Renew externally and restart. Clients and
`--tls-server-name` must use a name covered by the certificate and trust its issuer.
Both listeners use **implicit TLS** on container ports **1143** (IMAP) and
**1025** (SMTP). Change existing STARTTLS client settings. Healthcheck verifies
TLS and both greetings with bounded timeouts and needs no account credentials.
By default it trusts the active public certificate exported at `/data/tls-cert.pem`;
`--tls-cert` can select a public trust file. It reads no private key or vault key.
The generated private certificate key remains inside the encrypted vault; a
custom private key stays in its mounted file.

The Docker repository must explicitly configure the established service identity
and volume ownership; the base image's nonroot default can differ. Use a
read-only root, dropped capabilities, writable `/data` and private `/tmp`, mount
certificates read-only, and supply CA roots for outbound Proton HTTPS. Distroless
static supplies CA roots; `scratch` must copy a reviewed CA bundle explicitly.
Go timezone data is needed only if a deployment introduces named-zone behavior;
the smoke fixtures exercise UTC without an extra timezone database.

Connect consuming containers on a dedicated network that permits Proton
connections. Preserve host ports 10243/10125 mapped to 1143/1025 on IPv4 and IPv6
loopback only. Keep LAN/public publication disabled. Use local DNS/network aliases
covered by the certificate. Deployment settings live in the Docker repository.

The CLI supports password/TOTP, mailbox password and human verification.
Hardware-key-only authentication requires upstream FIDO2 support and is omitted.
Eligible paid Proton access remains required. Authentication expiry and network
failures appear in local logs. Protocol/body logging is disabled by default;
desktop integrations, OS keychains, automatic updates, and telemetry/crash
uploads and automatic TLS diagnostic uploads are disabled for this build.
User-requested bug reports remain available. Operators own security updates.

## Migration and release acceptance

Record the deployed image digest and actual version first: the inspected Compose
used 3.25.0 while its build declared 3.26.0. Record image size, native libraries,
idle memory, sync behavior and working clients before measuring savings.

Stop the old service and snapshot **all** state. In the old environment export
the existing decoded vault key without logging it; do not generate a replacement
key for an existing vault. Store it outside the state volume as the container
secret. For an existing headless installation, use its current `/data/vault.key`.
Preserve
vault, database, cache, local credentials and IMAP IDs. Never run the old/new
services together against the same state or use cloned authentication sessions.
Update client ports/TLS settings and start the replacement. Keep the old image
and snapshot until acceptance passes. Migrations and refresh-token rotation can
prevent image-only rollback: restore the complete snapshot when required and
expect possible reauthentication.

The inspected wrapper uses `pass` with the plaintext value encoded as standard
base64. Run this inside the **stopped old image's environment**, preserving its
`GNUPGHOME` and `PASSWORD_STORE_DIR`, with `/data` attached and no Bridge process:

```sh
set -euo pipefail
umask 077
key_file=$(mktemp /data/.vault.key.XXXXXX)
trap 'rm -f "$key_file"' EXIT
pass show 'docker-credential-helpers/cHJvdG9ubWFpbC9icmlkZ2UtdjMvdXNlcnMvYnJpZGdlLXZhdWx0LWtleQ==/bridge-vault-key' | base64 --decode > "$key_file"
test "$(wc -c < "$key_file")" -eq 32
chmod 700 /data
chmod 600 "$key_file"
sync -f "$key_file"
ln "$key_file" /data/vault.key
sync -f /data
```

The exclusive hard link refuses to replace an existing key. No key bytes are
printed. Verify the entry in the actual old environment before migration; a
different deployed wrapper may store its key elsewhere. Preserve the existing
`/data/config/protonmail/bridge-v3` state layout and service ownership.

The script above exports an intermediate `/data/vault.key` inside the old image.
Copy that same key into the Docker project's `secrets/vault_key`, apply its remapped
read ACL, and configure `--vault-key-file` before starting the new image. After
successful acceptance, remove the intermediate key from the state volume. Retained
old snapshots still contain unlocking material and require encrypted storage.

Before publishing a release, test with a live account and real clients:

- Password/TOTP, human verification, mailbox password, token refresh/restart,
  revoked sessions and reauthentication.
- Initial/incremental sync, folders/flags, attachments, send, offline reconnect,
  unchanged IMAP identities and Bridge passwords after migration.
- IPv4/IPv6 container and host-loopback access, absent LAN access, certificate
  hostname/trust validation and renewal on restart.
- Missing/truncated/wrong vault key and damaged vault preserve existing bytes;
  fresh initialization and interruption recovery; graceful shutdown, restart and
  snapshot rollback.
- Release checksum/provenance verification and consumption by the separate Docker
  build; review measured image/runtime savings and merge conflicts at upgrades.

Account/client acceptance is an operator gate, never an ordinary CI secret.
The first release remains unvalidated until these operator checks pass. The
running process still decrypts mail and holds credentials; no independent audit
or stronger mail encryption is claimed. Preserve GPL notices and corresponding
source when distributing this fork.
