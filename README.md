# Proton Mail Bridge
Copyright (c) 2026 Proton AG

## what is this fork about?

This fork turns Proton Mail Bridge into a minimal headless Linux amd64 service
for containers, retaining upstream's mail synchronization, IMAP/SMTP engine,
SQLite storage, and Proton API security checks.

We simplify the runtime by:

- Shipping one statically linked executable that runs directly as PID 1, with
  native listeners, healthchecks, and graceful shutdown. It needs no shell,
  process supervisor, `socat` forwarding, or runtime shared libraries and can
  run in Distroless or `scratch` containers.
- Excluding GUI, gRPC desktop IPC, desktop integrations, OS keychains, and FIDO2
  dependencies from the headless build. Password/TOTP login remains available;
  accounts requiring only hardware security keys need upstream Bridge.
- Replacing the `pass`/GPG/keychain stack with a vault key supplied as a read-only
  container secret, outside the mail state volume. The vault and message-content
  files use encryption; SQLite metadata and logs are plaintext. Encrypt state
  storage and backups as well. A local `0600` key file remains available for
  standalone operation.
- Disabling automatic updates and automatic telemetry, crash, and TLS diagnostic
  uploads. Operators deploy reviewed releases; explicit bug reports remain
  available.

The separate secret trades keychain unlock policies for host filesystem permissions
and container isolation. Keeping the key outside the mail state volume lets us
back it up separately, but Compose file secrets are not encrypted storage and do
not guarantee access only by Bridge. Anyone able to read both the key and the
encrypted data can decrypt it; protect the host, secret, and backups accordingly.

We harden the service by requiring TLS for mail connections,
preventing concurrent access to the same state, and stopping
startup on invalid vault keys, damaged vaults, or invalid configured certificates
without resetting existing state. Deployment uses a dedicated container network,
host ports published only on loopback, a nonroot service identity, a read-only
root filesystem, and dropped capabilities. Build inputs are pinned, and release
checks cover dependencies, vulnerabilities, secrets, and runtime behavior.

Both mail listeners require **implicit TLS**: encryption starts before any
IMAP/SMTP greeting or authentication. Clients must select **SSL/TLS** on container
ports **1143** (IMAP) and **1025** (SMTP), and validate certificate trust and
hostname. [RFC 8314](https://www.rfc-editor.org/rfc/rfc8314.html#section-3)
recommends this mode for mail client access and submission. STARTTLS begins with
plaintext protocol negotiation and upgrades the connection before login; properly
enforced STARTTLS uses the same TLS cryptography and protects credentials equally
well. Our implicit TLS requirement removes the plaintext startup phase and makes
encryption mandatory. It also avoids patching the pinned upstream Gluon IMAP
engine, which permits login before STARTTLS and exposes no setting to require
the upgrade. Upstream Bridge supports both modes; this fork requires its existing
implicit TLS mode for container connections.

For build, operation, migration, and acceptance details, see
[HEADLESS.md](HEADLESS.md). Live account/client authentication and sending remain
acceptance requirements; successful TLS handshakes alone do not verify them.
The sections below describe the upstream desktop application retained in this
repository.

## Vault key secret

For **fresh state only**, create 32 random raw bytes in the Docker project's
ignored secrets directory. Run on the Docker host from `/opt/docker/protonmail-bridge`:

```sh
mkdir -p -m 0700 secrets
(umask 077; set -C; openssl rand 32 > secrets/vault_key)
```

`set -C` refuses to overwrite an existing file. For existing state, export or
copy its existing vault key instead: generating a different key would make the
vault unreadable. Keep a separately encrypted recovery copy of this key.

Supply the file using Compose:

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

The file is mounted read-only. An explicitly supplied key must already exist
and contain exactly 32 bytes; Bridge never generates it, changes its permissions,
copies it into `/data`, or falls back to a local key on failure. Owner read access
is required; group or named ACL read access is allowed, but executable bits,
group write, and all other-user permissions are rejected. File-source Compose
secrets retain host ownership and ACLs; setting Compose `uid`, `gid`, or `mode`
does not fix host permissions.

Our Puppet node configuration in `data/nodes/docker.home.arpa.yaml` grants host
UID **101000** (remap base 100000 + container UID 1000) directory traversal and
read access to `/opt/docker/protonmail-bridge/secrets/vault_key`. The named ACL
reports mode `0640` because the group bits represent the ACL mask; the owning
group has no access. Apply the normal Puppet configuration before starting the
service. The headless test Compose file in the Docker repository uses this secret.

Back up mail state and the key separately. Compose file secrets are host bind
mounts, not encrypted storage. State-volume backups exclude this external key;
whole-host backups can still contain both. Remove old key copies from the state
volume after a successful migration; mounting a secret does not remove them.

This repository holds the Proton Mail Bridge application.

For a detailed build information see [BUILDS](./BUILDS.md).
The license can be found in [LICENSE](./LICENSE) file, for more licensing information see [COPYING_NOTES](./COPYING_NOTES.md).
For contribution policy see [CONTRIBUTING](./CONTRIBUTING.md).


## Description Bridge
Proton Mail Bridge for e-mail clients.

When launched, Bridge will initialize local IMAP/SMTP servers and render 
its GUI.

To configure an e-mail client, first log in using your Proton Mail credentials. 
Open your e-mail client and add a new account using the settings which are 
located in the Bridge GUI. The client will only be able to sync with 
your Proton Mail account when the Bridge is running, thus the option 
to start Bridge on startup is enabled by default.

When the main window is closed, Bridge will continue to run in the
background.

More details [on the public website](https://proton.me/mail/bridge).

## Launcher
The launcher is a binary used to run the Proton Mail Bridge.

The Official distribution of the Proton Mail Bridge application contains
both a launcher and the app itself. The launcher is installed in a protected
area of the system (i.e. an area accessible only with admin privileges) and is
used to run the app. The launcher ensures that nobody tampered with the app's
files by verifying their signature using a hardcoded public key. App files are
placed in regular userspace and are signed by Proton's private key. This
feature enables the app to securely update itself automatically without asking
the user for a password.

## Keychain
You need to have a keychain in order to run Proton Mail Bridge. On Mac or
Windows, Bridge uses native credential managers. On Linux, use `secret-service` freedesktop.org API
(e.g. [Gnome keyring](https://wiki.gnome.org/Projects/GnomeKeyring/))
or
[pass](https://www.passwordstore.org/). We are working on allowing other secret
services (e.g. KeepassXC), but for now only gnome-keyring is usable without
major problems.


## Environment Variables

### Dev build or run
- `APP_VERSION`: set the bridge app version used during testing or building
- `PROTONMAIL_ENV`: when set to `dev` it is not using Sentry to report crashes
- `VERBOSITY`: set log level used during test time and by the makefile

### Integration testing
- `TEST_ENV`: set which env to use (fake or live)
- `TEST_ACCOUNTS`: set JSON file with configured accounts
- `TAGS`: set build tags for tests
- `FEATURES`: set feature dir, file or scenario to test

## Folders

There are now three types of system folders which Bridge recognises:

|        | Windows                             | Mac                                                 | Linux                               | Linux (XDG)                           |
|--------|-------------------------------------|-----------------------------------------------------|-------------------------------------|---------------------------------------|
| config | %APPDATA%\protonmail\bridge-v3      | ~/Library/Application Support/protonmail/bridge-v3  | ~/.config/protonmail/bridge-v3      | $XDG_CONFIG_HOME/protonmail/bridge-v3 |
| cache  | %LOCALAPPDATA%\protonmail\bridge-v3 | ~/Library/Caches/protonmail/bridge-v3               | ~/.cache/protonmail/bridge-v3       | $XDG_CACHE_HOME/protonmail/bridge-v3  |
| data	  | %APPDATA%\protonmail\bridge-v3      | ~/Library/Application Support/protonmail/bridge-v3  | ~/.local/share/protonmail/bridge-v3 | $XDG_DATA_HOME/protonmail/bridge-v3   |
| temp   | %LOCALAPPDATA%\Temp                 | $TMPDIR if non-empty, else /tmp                     | $TMPDIR if non-empty, else /tmp     | $TMPDIR if non-empty, else /tmp       |



## Files

|                        | Base Dir | Path                       |
|------------------------|----------|----------------------------|
| bridge lock file       | cache    | bridge.lock                |
| bridge-gui lock file   | cache    | bridge-gui.lock            |
| vault                  | config   | vault.enc                  |
| gRPC server json       | config   | grpcServerConfig.json      |
| gRPC client json       | config   | grpcClientConfig_<id>.json |
| gRPC Focus server json | config   | grpcFocusServerConfig.json |
| Logs                   | data     | logs                       |
| gluon DB               | data     | gluon/backend/db           |
| gluon messages         | data     | gluon/backend/store        |
| Update files           | data     | updates                    |
| sentry cache           | data     | sentry_cache               |
| Mac/Linux File Socket  | temp     | bridge{4_DIGITS}           |
