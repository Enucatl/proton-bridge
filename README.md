# QuarkBridge - the minimalistic container ProtonBridge

[![image](https://img.shields.io/badge/image-ghcr.io%2Fenucatl%2Fproton--bridge-2496ED?logo=docker&logoColor=white)](https://github.com/Enucatl/proton-bridge/pkgs/container/proton-bridge)
[![latest tag](https://ghcr-badge.egpl.dev/enucatl/proton-bridge/latest_tag?label=latest&ignore=latest,sha256*)](https://github.com/Enucatl/proton-bridge/pkgs/container/proton-bridge)
[![image size](https://ghcr-badge.egpl.dev/enucatl/proton-bridge/size?tag=latest&label=image%20size)](https://github.com/Enucatl/proton-bridge/pkgs/container/proton-bridge)
[![downloads](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fghcr-badge.elias.eu.org%2Fapi%2FEnucatl%2Fproton-bridge%2Fproton-bridge&query=downloadCount&label=docker%20pulls&color=2496ED&logo=docker&logoColor=white)](https://github.com/Enucatl/proton-bridge/pkgs/container/proton-bridge)
[![build](https://img.shields.io/github/actions/workflow/status/Enucatl/proton-bridge/headless.yml?branch=main&label=build)](https://github.com/Enucatl/proton-bridge/actions/workflows/headless.yml)
[![scan](https://img.shields.io/badge/scan-Trivy-1904DA?logo=trivy&logoColor=white)](https://github.com/Enucatl/proton-bridge/actions/workflows/headless.yml)
[![security](https://img.shields.io/badge/vulnerabilities-GitHub%20Security-2EA44F?logo=github&logoColor=white)](https://github.com/Enucatl/proton-bridge/security/code-scanning)

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
- Remove the "funny" `crash@bandicoot` login that lets anyone crash your Bridge over the network without authenticating.
- Pin build inputs and check dependencies, vulnerabilities, secrets, and runtime behavior.
- Sync faster thanks to improved concurrency.

## Usage

### 1. Generate the secret key

For a fresh installation, generate a 32-byte key and grant the container's
mapped host UID read access. This example uses `101000` for container UID `1000`
with a remap base of `100000`; use your mapped UID (`1000` without remapping):

```sh
mkdir -p -m 0700 secrets
(umask 077; set -C; openssl rand 32 > secrets/vault_key)
sudo setfacl -m u:101000:--x secrets
sudo setfacl -m u:101000:r-- secrets/vault_key
```

Keep this key when reusing existing data; a new key cannot decrypt it.

### 2. Run and log in

Save the Compose example below as `compose.yaml`, then open the interactive CLI:

```sh
docker compose stop bridge
docker compose run --rm bridge --cli --vault-key-file /run/secrets/bridge_vault_key
```

At the Bridge prompt, run `login` and follow the password prompts. For two-factor
authentication, only TOTP is supported. Then run `info 0` to get your mail client
credentials and `exit` to close the CLI.

### 3. Run noninteractive with Docker Compose

```yaml
services:
  bridge:
    image: ghcr.io/enucatl/proton-bridge:latest
    platform: linux/amd64
    user: "1000:1000"
    command: ["--noninteractive", "--vault-key-file", "/run/secrets/bridge_vault_key"]
    restart: unless-stopped
    stop_grace_period: 20s
    read_only: true
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    ports:
      - "10243:1143"
      - "10125:1025"
    volumes:
      - bridge_data:/data
      # Optional: mount existing certificates, readable by the mapped container UID.
      # If omitted, Bridge creates and persists a self-signed certificate.
      # - ./certs/cert.pem:/protonmail/certs/cert.pem:ro
      # - ./certs/key.pem:/protonmail/certs/key.pem:ro
    tmpfs:
      - /tmp:rw,noexec,nosuid,uid=1000,gid=1000,mode=700
    secrets:
      - bridge_vault_key

volumes:
  bridge_data:

secrets:
  bridge_vault_key:
    file: ./secrets/vault_key
```

```sh
docker compose up -d
```

Connect your mail client to the Bridge host, with IMAP on `10243` and SMTP on
`10125`, using **SSL/TLS** and the credentials from `info 0`. Use a hostname
covered by your mounted certificate. For the default self-signed certificate,
connect to `127.0.0.1` and trust it in your mail client.

## Upstream releases

The stable release sync runs daily at **05:47 UTC** and on manual dispatch.
It merges the exact latest stable ProtonMail release tag into a temporary
`bot/upstream-release/<tag>` branch and proposes a PR to `main`. Releases already
contained in `main` or an open release PR are skipped. Upstream development
commits wait for a stable release.

Codex runs only for merge conflicts, with one attempt and a 15-minute agent limit,
using the baseline's `deepseek/deepseek-v4.1-flash` model through OpenRouter's
Responses endpoint. It may change only conflicted paths. Automation and
security-policy conflicts require manual repair. Failed attempts retain
diagnostics as workflow artifacts for 14 days and open no PR; the next run retries.
The PR includes pinned commits and the conflict-resolution summary for review.

Configure repository Actions secrets `OPENROUTER_API_KEY` and
`UPSTREAM_SYNC_TOKEN`. The latter must be a repository-scoped fine-grained token
with **Contents**, **Pull requests**, and **Workflows** write permissions so
the resulting PR starts CI. The write token is available only to the trusted PR
creation step, after a separate job reconstructs and validates the merge.

Require human approval and successful existing CI before merging a release PR.
Use GitHub's **Create a merge commit** method to retain upstream ancestry, then
delete the temporary branch. A squash or rebase merge prevents the next sync
from recognizing the release as integrated. This repository permits merge
commits only, deletes merged branches automatically, and requires passing
`check`, `image / build / finalize`, and `image / scan` checks on `main`, including
for administrators. Direct pushes must therefore have passing checks first.
Review remains manual: the maintainer's sync token creates PRs under their own
account, and GitHub does not allow authors to formally approve their own PRs.

Every checked push to `main`, including fork fixes between releases, continues
publishing the Makefile version, major/minor aliases, and `latest`. No separate
stable branch or release snapshot image is built.

Copyright (c) 2026 Proton AG
