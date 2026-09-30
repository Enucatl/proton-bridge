# Proton Mail Bridge headless

This fork distributes one Linux amd64 executable based on upstream `v3.27.1`.
GitHub Releases on `Enucatl/proton-bridge` are the canonical binary/source
distribution. The separate Docker repository packages pinned releases and owns
Compose, image scans, publication, and deployment. The Dockerfiles here are
build and validation fixtures.

## Build and verify

Docker, Bash, OpenSSL, Git, `strings` (binutils), and an amd64 host are required:

```sh
make headless-check
make headless-smoke
make headless-package HEADLESS_VERSION=v3.27.1-dev
./scripts/headless/security.sh source
./scripts/headless/security.sh artifacts headless-dist/release/proton-bridge-headless-v3.27.1-dev.sbom.json
```

`scripts/headless/environment.sh` pins Go 1.26.7 and the builder/runtime image
digests. The builder pins compiler and tooling package versions; unavailable
pins fail the build rather than selecting newer versions. Build with CGO and
musl to retain the upstream SQLite implementation. Go's DNS/user lookup uses
its native implementations and SQLite extension loading is disabled. The ELF
gate rejects an interpreter or shared-library dependency. No runtime shared
libraries are required by a passing artifact. This does not remove CGO.

The checks cover the compiled application graph, affected-package races, vet,
formatting and reachable dependency vulnerabilities. Relevant upstream engine
integration tests in `internal/bridge` exercise fake Proton APIs with real
IMAP/SMTP sockets, SQLite and synchronization/send state. The desktop Godog/UI
harness is preserved outside the container graph. Desktop source and upstream
GitLab files remain for upgrades. Smoke tests run the exact release executable
in pinned Distroless `static-debian13:nonroot` and `scratch`, with numeric UID/GID
1000, read-only root, no capabilities, private writable state and temporary
directories. The test-only probe validates DNS, outbound certificate verification,
SQLite writes, and both local TLS protocol greetings over IPv4 and IPv6; it is excluded from release
archives. Service checks cover CLI, healthcheck, exclusive state lock and SIGTERM.
They also check self-signed startup without a certificate mount, unchanged
certificate identity after restart, and rejection of explicitly invalid paths/PEM.
Network-dependent checks need outbound DNS/HTTPS; failures block publication.
These local bind-mount fixtures use the host user namespace to accommodate Docker
daemons with UID remapping. They do not set deployment namespace policy.

The pipeline also uses a version/digest-pinned Trivy scanner with the security
baseline's `HIGH,CRITICAL` policy: source misconfigurations and secrets, compiled
binary vulnerabilities and secrets, and release SBOM vulnerabilities. Fixed
vulnerabilities at that severity block publication; scanner errors also fail the
job. JSON/SARIF reports are retained as workflow artifacts. `govulncheck` separately
checks reachable Go vulnerabilities. Trivy identifies Go dependencies from the
binary and SBOM; the static native components remain listed in the SBOM and need
native-advisory review because this scan cannot identify them reliably. Deployment
image scanning stays in the Docker repository.

The tracked `.githooks/pre-push` hook runs the same source scan in Docker before
a local push and blocks findings or scanner errors. It scans the current working
tree; CI scans the checked-out revision and the built binary/SBOM. It is enabled
in this checkout. To enable it in a fresh clone, run:

```sh
git config --local core.hooksPath .githooks
```

Docker must be available for the push check. The hook creates no commits or tags
and does not change commit/tag signing.

Local validation on 2026-09-30 passed both shell-free fixtures. The measured
uncompressed executable was 25,686,768 bytes. The validation images measured
35,589,270 bytes (Distroless) and 33,399,356 bytes (`scratch`), including the
test-only probe; these are not production image sizes. The checked application
graph had no reachable vulnerabilities reported by `govulncheck`. A deployed
baseline and live Proton/client acceptance remain outstanding; no saving is
claimed against the existing deployment.

The changes live directly in this fork, and builds compile its source. To
upgrade, merge or rebase a newer upstream release into the downstream branch,
resolve conflicts, and rerun the build and acceptance gates.

Build output lives in `headless-dist`. The workflow archives the binary it tested
without rebuilding, includes dependency license/notice texts and an application
SBOM including the statically linked musl/GCC runtime and embedded SQLite,
and archives the exact committed fork source with `go.mod`/`go.sum`.
For a source archive, set `REVISION`, `SOURCE_DATE_EPOCH`, and `HEADLESS_VERSION`
from its build metadata before executing `build.sh` in the pinned builder;
`build.sh` does not require Git when these values are supplied. `run.sh` mounts
local source in the build environment. The full CI pipeline requires a checkout
because corresponding source is generated with `git archive HEAD`.

Releases reuse upstream tags such as `v3.27.1`, preserving their original Git
targets. After live acceptance, run the **Headless Linux distribution** workflow
on `master` with `upstream_tag=v3.27.1`, or use:

```sh
gh workflow run headless.yml --repo Enucatl/proton-bridge --ref master -f upstream_tag=v3.27.1
```

The workflow builds the fork commit on `master`, validates that the selected tag
matches `UPSTREAM_VERSION` and is an ancestor of that commit, and publishes assets
under that existing tag. GitHub's automatic source links refer to upstream;
the attached `proton-bridge-headless-v3.27.1-source.tar.gz` and build metadata
identify the actual fork source used for the executable. No upstream tag is moved.
An empty `upstream_tag` runs checks without publishing. PR checks have read-only
permissions and no release/account secrets. Failed gates prevent release creation.
The release job downloads that run's checked artifact, verifies checksums,
attaches signed GitHub build provenance, creates a draft, then publishes after all
preceding steps pass. Local build commands create no tag or release.

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
