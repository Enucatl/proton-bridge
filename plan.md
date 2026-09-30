# Minimal Proton Bridge distribution

## Feasibility and agreed outcome

Build a headless Linux service from Proton Bridge, keeping its mail engine
close to upstream and concentrating changes at startup, networking, and local
key storage. This is feasible without reimplementing Proton authentication,
cryptography, synchronization, IMAP, or SMTP. Those components remain substantial;
the goal is the smallest maintainable distribution supporting this deployment.

The first artifact is this plan. Implementation and publication follow separately.
The assessment used upstream checkout `b9c5dac1` and the deployment found at
`/export/docker/protonmail-bridge`, corresponding to the intended deployment at
`/opt/docker/protonmail-bridge`. No replacement binary or migration has been
validated yet.

Agreed defaults:

- Linux amd64 initially; retain upstream source/history and a small patch set.
- One executable named `proton-bridge-headless`, with noninteractive operation,
  an interactive provisioning CLI, and a healthcheck mode.
- Password/TOTP, mailbox-password, and human-verification login flows; omit FIDO2.
- IMAP and SMTP use implicit TLS. Clients must change their STARTTLS settings.
- Docker clients and host loopback access; no LAN or public port publication.
- Store the vault key inside `/data`, alongside encrypted state.

## Deliverables and repository ownership

This repository produces the binary. GitHub Releases on
`Enucatl/proton-bridge` are the canonical distribution channel. The separate
Docker repository consumes a pinned release archive and verified checksum;
it packages the runtime image and owns deployment-specific Compose settings.
Do not maintain two independent builds of the mail application.

- `upstream`: `git@github.com:ProtonMail/proton-bridge.git`.
- `origin`: `git@github.com:Enucatl/proton-bridge.git`.
- Preserve existing upstream tags. Downstream tags use
  `headless-v<upstream-version>-<revision>`, starting with
  `headless-v3.27.1-1` after validation.
- Retain the current history and this plan commit. Base implementation on the
  `v3.27.1` release content rather than unreleased master changes.
- Keep upstream module paths. Embed upstream version, downstream version, and
  commit revision in version output; preserve the upstream API version identity.

Each downstream release contains:

- `proton-bridge-headless-<release>-linux-amd64.tar.gz`, containing the executable,
  license notices, and operating instructions.
- `SHA256SUMS`, an SBOM, and build provenance.
- `proton-bridge-headless-<release>-source.tar.gz`, containing the exact patched
  source, dependency locks, and build instructions needed to reproduce the build.
- Release notes stating the upstream base, changes, runtime libraries, client TLS
  requirements, and state migration/rollback constraints.

## Implementation

### Preserve the upstream engine

Retain Proton API clients, SRP/authentication and token refresh, cryptography,
MIME handling, Gluon, SMTP submission, synchronization/event processing, encrypted
cache, database, vault serialization, migrations, and account/address modes.
Unused serialized fields remain for compatibility.

Retain remote feature flags: they affect protocol and synchronization behavior.
Disable telemetry/crash uploads at existing sending boundaries, retaining internal
interfaces where removing them would spread changes into mail logic. Preserve
Proton TLS verification/pinning and other security defenses.

### Container build and lifecycle

Introduce one container build tag. Reuse existing startup and provisioning code;
exclude Qt, GUI gRPC, desktop focus/autostart, OS keychain backends, and FIDO2 from
the compiled dependency graph. Preserve unused upstream source outside that graph
to keep upgrades manageable. Remove CLI commands for omitted desktop features.

Build the application directly without the launcher. Disable update downloads and
installation; Docker image releases replace application self-update. Keep local
logging, with mail protocol/body logging disabled by default.

Retain `--noninteractive` and `--cli`; add `--healthcheck`. Handle SIGTERM/SIGINT
through graceful Bridge shutdown, preserve an exclusive state lock, and exit
nonzero on startup failure. Compose executes the binary directly as PID 1.

### Vault key

Replace OS keychain discovery/loading with a Go file operation using
`/data/vault.key`: exactly 32 cryptographically random bytes, generated once on
fresh initialization with exclusive creation and mode `0600`. Keep its parent
directory private. Complete and sync the key write before creating the vault;
reuse a valid key if initialization was interrupted before vault creation.

An existing vault with a missing, malformed, or incorrect key must stop startup
without changing state. Disable insecure-key fallback and automatic vault reset
on decryption/unmarshal failure. Preserve the vault's current encryption and
atomic-write implementation. Never automatically regenerate a lost key.

The inspected upstream vault loader resets an unreadable vault; this behavior
must be changed before migrating key storage. Bridge's vault key is symmetric.
The current wrapper's GPG pair protects that key through `pass`, and its
unprotected private key is also in `/data`. Replacing this arrangement with a
private key file preserves the chosen unattended protection boundary. A full
volume backup includes the decryption key and must be treated accordingly.

### Native IMAP/SMTP and certificates

Change the shared mail listener, not the application-wide localhost constant.
Bind directly on container interfaces with IPv4/IPv6 support. Remove both
`socat` forwarding processes and their orchestration.

Use existing TLS listeners for both services and disallow plaintext operation
in the container variant. This avoids patching Gluon, whose inspected version
permits IMAP login before STARTTLS without an exposed enforcement option.
Also disable SMTP's insecure-authentication allowance.

Use native container ports `1143` (IMAP) and `1025` (SMTP). Preserve host ports
`10243` and `10125`, published only on IPv4/IPv6 loopback. A dedicated Docker
network connects consuming containers and permits outbound Proton connections.
Document the changed container ports and implicit TLS client settings.

Reuse the certificate/key mounted under `/protonmail/certs`. Load them through
the existing certificate implementation without shell-driven import. Validate
configured files before serving; missing or invalid files stop startup instead
of falling back to a self-signed certificate. Restart after renewal. Clients
use a certificate-covered hostname with suitable local DNS/network aliases.

### Runtime packaging

Ship the binary, CA roots, and only its verified runtime libraries. Remove Bash,
GPG/pass, socat, Secret Service/DBus, Qt, and FIDO2 packages. Keep the existing
non-root identity, read-only root filesystem, dropped capabilities, and writable
state volume. Preserve SQLite/CGO; do not rewrite the database to obtain a static
binary. Publish the actual linked-library requirements and validate the binary
in the intended minimal runtime.

The executable healthcheck completes TLS handshakes and reads both protocol
greetings with bounded timeouts. It does not require account credentials.
Authentication expiry and Proton connectivity errors remain visible in logs.

## GitHub Actions build, checks, and release

Add fork-specific GitHub Actions workflows; preserve upstream GitLab files.
Pin action revisions, Go toolchain, and build environment. Dependency security
overrides belong in reviewed module changes, not build-time `go get` commands.

- Pull requests and pushes to the downstream branch run formatting checks,
  relevant upstream unit/integration tests, focused container-variant tests,
  race checks for affected packages, `go vet`, dependency vulnerability checks,
  and an amd64 build plus runtime smoke test.
- Verify the compiled dependency graph/linkage excludes the removed native
  backends and GUI. Exercise healthcheck, TLS listeners, and shutdown in the
  intended runtime without requiring a live Proton account.
- Upload the checked binary as a temporary workflow artifact for review.
- A pushed `headless-v*` tag runs the same gates, builds the release archive,
  generates checksums/SBOM/provenance, and creates a draft GitHub Release.
- Publish the draft automatically only after all gates and artifact generation
  succeed. Upload the exact tested artifact; do not rebuild after testing.
- Publish with narrowly scoped release permissions; PR jobs have read-only
  permissions and no account/release secrets. Release jobs cannot use untrusted
  PR code. Do not let remote cache/artifact names select an unverified binary.
- Live Proton/client acceptance is a separate operator check before creating
  a release tag, avoiding credentials in ordinary CI.

The Docker repository downloads an explicit release version, verifies its
checksum, builds and scans its image, and publishes a pinned image. Updating
the release pin is an explicit reviewed change. No deployment occurs merely
because this repository publishes a binary.

## Migration and verification

First establish the actual deployed image digest/version: inspected Compose
specifies `3.25.0`, while the build version specifies `3.26.0`. Record baseline
image size, native libraries, idle memory, sync behavior, and working clients.

Stop the old service and snapshot all state. Use the old environment to export
the existing decoded vault key directly into the private new key file without
logging it. Preserve vault, database, cache, local credentials, and IMAP IDs.
Start the replacement and update client ports/TLS settings. Never run both
versions against the same state or concurrently use cloned authentication
sessions. Keep the old image and snapshot until acceptance completes.

Required acceptance scenarios:

- Login/TOTP, human verification, mailbox password, refresh-token rotation,
  restart, session revocation, and reauthentication.
- Initial/incremental sync, folders/flags, attachments, sending, offline
  reconnect, and stable IMAP identities/Bridge passwords after migration.
- Missing/truncated/wrong key and damaged vault leave existing bytes untouched.
  Fresh initialization and interruption before vault creation recover safely.
- Plaintext cannot authenticate; TLS validates certificate trust/hostname;
  invalid/missing custom certificates prevent startup; renewal works on restart.
- IPv4/IPv6 Docker and host-loopback connections work; LAN access is unavailable.
- Graceful shutdown, state-lock exclusion, healthcheck failures, restart recovery,
  and snapshot restore/rollback.
- Release workflow cannot publish when checks fail; archives/checksums and Docker
  consumption work; documented native libraries match actual runtime linkage.
- Replay the patch set across an upstream release boundary and record conflicts.

## Risks and maintenance policy

Smaller packages/process count reduces some exposure; mandatory TLS and limited
reachability supply concrete additional protections. The running process still
decrypts mail and holds credentials. A compromised host/container remains able
to access them. Do not claim an independent security audit or stronger mail
encryption from this change.

Preserve a few ordered commits for lifecycle, key storage, listeners/TLS, and
packaging/CI. Upgrade by replaying them onto upstream releases and running the
gates above. Review security releases promptly: Proton can reject obsolete
clients, and removing self-update transfers update responsibility to us.

State migrations and refresh-token rotation can prevent image-only rollback;
restore the snapshot when required and expect possible reauthentication.
Hardware-key-only login requires restoring FIDO2 support. Eligible paid Proton
access remains required. Preserve GPL/license notices and corresponding source
for distributed binaries.

Measure savings after implementation; do not promise an image size, CPU/memory
reduction, or CGO-free binary before measuring. Proceed if acceptance passes and
the patch set remains concentrated at application boundaries. Changes expanding
into mail synchronization, cryptography, or a Gluon fork require reassessment.
