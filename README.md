# Proton Mail Bridge - QuarkBridge

[![image](https://img.shields.io/badge/image-ghcr.io%2Fenucatl%2Fproton--bridge-2496ED?logo=docker&logoColor=white)](https://github.com/Enucatl/proton-bridge/pkgs/container/proton-bridge)
[![latest tag](https://img.shields.io/github/v/release/ProtonMail/proton-bridge?label=latest&color=2496ED)](https://github.com/Enucatl/proton-bridge/pkgs/container/proton-bridge)
[![image size](https://img.shields.io/badge/image%20size-19.5%20MB-2496ED)](https://github.com/Enucatl/proton-bridge/pkgs/container/proton-bridge)
[![downloads](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fghcr-badge.elias.eu.org%2Fapi%2FEnucatl%2Fproton-bridge%2Fproton-bridge&query=downloadCount&label=docker%20pulls&color=2496ED&logo=docker&logoColor=white)](https://github.com/Enucatl/proton-bridge/pkgs/container/proton-bridge)
[![build](https://img.shields.io/github/actions/workflow/status/Enucatl/proton-bridge/headless.yml?branch=main&label=build)](https://github.com/Enucatl/proton-bridge/actions/workflows/headless.yml)
[![scan](https://img.shields.io/badge/scan-Trivy-1904DA?logo=trivy&logoColor=white)](https://github.com/Enucatl/proton-bridge/actions/workflows/headless.yml)
[![security](https://img.shields.io/badge/vulnerabilities-GitHub%20Security-2EA44F?logo=github&logoColor=white)](https://github.com/Enucatl/proton-bridge/security/code-scanning)

Copyright (c) 2026 Proton AG

## What is this fork about?

Run Proton Mail Bridge as a minimal headless containerized service,
while retaining upstream mail synchronization, IMAP/SMTP, SQLite storage, and
Proton API security checks.

## Principles

### Simplifications

- One process runs as PID 1, with native listeners, healthchecks, and graceful shutdown.
- Distroless runtime with no shell, supervisor, or `socat` forwarding.
- No GUI, desktop IPC, desktop integrations, OS keychain, or FIDO2 dependencies.
- A vault key file replaces the `pass`/GPG/keychain stack.
- Operators deploy updates; Bridge does not update itself.

### Improvements

- Require implicit TLS for every IMAP and SMTP connection.
- Reject invalid keys, damaged vaults, and invalid configured certificates without resetting state.
- Prevent concurrent processes from accessing the same state.
- Deploy as nonroot with a read-only root filesystem, dropped capabilities
- Disable automatic telemetry, crash reports, and TLS diagnostic uploads.
- Pin build inputs and check dependencies, vulnerabilities, secrets, and runtime behavior.
- Sync faster thanks to improved concurrency.

## Usage

### Requirements

Use an eligible Proton account. Password/TOTP is supported; accounts requiring only hardware security keys need
upstream Bridge.

The container runs as UID/GID **1000:1000** and stores state in **/data**. Make that
volume private and writable by the service identity. Full deployment settings
live in the separate Docker Compose repository.

### Vault key secret

No OS keychain is required. Container deployments supply an existing **32-byte
raw key** using `--vault-key-file`.

For **fresh state only**, run from `/opt/docker/protonmail-bridge` on the Docker
host to create the key in the project's ignored secrets directory:

```sh
mkdir -p -m 0700 secrets
(umask 077; set -C; openssl rand 32 > secrets/vault_key)
```

`set -C` refuses to overwrite an existing file. For existing state, copy or export
its existing key: a new key cannot decrypt the vault. Follow the
[migration instructions](#migration).

The key must be readable by its owner and the container user. Group or named ACL
read access is allowed; executable bits, group write, and other-user permissions
are rejected. Compose file secrets retain host ownership and ACLs; Compose
`uid`, `gid`, and `mode` settings do not fix host permissions.

For our deployment, Puppet's `data/nodes/docker.home.arpa.yaml` grants host UID
**101000** (remap base 100000 + container UID 1000) directory traversal and read
access to the key. Apply that configuration before starting Bridge. Its named
ACL reports mode `0640`; the group bits are the ACL mask, and the owning group
has no access.

### Docker Compose

Add the secret to the existing Bridge service:

```yaml
services:
  bridge:
    command: ["--noninteractive", "--vault-key-file", "/run/secrets/bridge_vault_key"]
    secrets:
      - bridge_vault_key
secrets:
  bridge_vault_key:
    file: ./secrets/vault_key
```

Bridge reads the mounted key without modifying it or copying it into `/data`.
A missing, malformed, or unsafe secret stops startup; there is no fallback key.
Standalone operation without `--vault-key-file` uses a local `0600` key file.

Use a dedicated container network with outbound Proton access, a read-only root
filesystem, dropped capabilities, and private writable `/data` and `/tmp`.
Publish host ports only on loopback.

### Account and mail client setup

Provision the account with `--cli`, using the same state directory and vault key
as the service. Stop the service before opening the CLI; only one process may
access the state at a time. Then start the service with `--noninteractive`.

Configure mail clients with the credentials provided by Bridge and **SSL/TLS**:

| Protocol | Container port | Host loopback port in our deployment |
|----------|----------------|--------------------------------------|
| IMAP     | 1143           | 10243                                |
| SMTP     | 1025           | 10125                                |

Both listeners require implicit TLS; STARTTLS is not supported. Clients must
trust the certificate and validate its hostname. Mount a certificate chain and
private key with `--tls-cert` and `--tls-key`, or explicitly trust Bridge's
persistent self-signed certificate, whose default identity covers `127.0.0.1`.

The built-in healthcheck verifies TLS and protocol greetings. Verify login,
synchronization, and sending separately with a live account and real clients.

### Backups

Back up state and the vault key separately, encrypting both backups. The vault
and message-content files are encrypted; SQLite metadata and logs are plaintext.
Compose file secrets are host bind mounts, not encrypted storage. Anyone with
both the key and encrypted data can decrypt it.

After a successful migration, remove old key copies from the state volume.
Mounting a secret does not remove them, and whole-host backups may contain both
state and key.

### Migration

1. Stop the old service and snapshot all state; keep its image for rollback.
2. Copy the existing headless `/data/vault.key` into `secrets/vault_key`. For a
   `pass`/GPG installation, export and decode its existing vault key in the old
   environment. The resulting file must contain exactly 32 raw bytes.
3. Grant the container user read access and configure the Compose secret.
4. Start the replacement with the original state and update clients to SSL/TLS.
5. Verify login, synchronization, sending, and restart before removing old key copies.

Never run both versions against the same state or use cloned sessions concurrently.
Rollback may require restoring the full snapshot and authenticating again.

### Build and verify

With Docker, Bash, OpenSSL, Git, and an amd64 host:

```sh
docker build --platform linux/amd64 --build-arg REVISION="$(git rev-parse HEAD)" \
    -f scripts/headless/Dockerfile.image -t protonmail-bridge-headless:local .
make headless-check
make headless-smoke
```

Checks cover the mail engine, races, dependencies, and container operation.
Live account and client checks remain separate. Use `--no-cache` when building
to refresh Debian package fixes.

### Development and project reference

See [BUILDS.md](BUILDS.md) for upstream build information and
[CONTRIBUTING.md](CONTRIBUTING.md) for contribution policy.
Licensing details are in [LICENSE](LICENSE) and [COPYING_NOTES.md](COPYING_NOTES.md).

Build and test environment variables:

| Variable | Purpose |
|----------|---------|
| `APP_VERSION` | Bridge version used during testing or building |
| `PROTONMAIL_ENV` | Set to `dev` to disable Sentry in development builds |
| `VERBOSITY` | Log level for tests and the Makefile |
| `TEST_ENV` | Integration test environment (`fake` or `live`) |
| `TEST_ACCOUNTS` | JSON file containing test accounts |
| `TAGS` | Build tags for tests |
| `FEATURES` | Feature directory, file, or scenario to test |
