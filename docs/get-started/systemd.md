---
title: Run with systemd
description: "Install the provider as a hardened systemd service on each control-plane node, then run the generated node setup phases to install its files, check it, and start it."
eyebrow: Get started · Step 5
weight: 50
verifiedBy:
  - deploy/systemd/bao-kms-provider.service
  - deploy/package/linux
  - deploy/config/provider-systemd.yaml
  - test/deployment/systemd-install.sh
  - internal/scaffold/nodescript.go
---

Repeat this procedure on every control-plane node. At the end, the provider
runs as the non-root `openbao-kms` user, listens on
`/run/openbao-kms/kms.sock`, and has passed `doctor` against OpenBao.

## Before you begin

- Download and verify the artifact in
  [Download the release](/docs/get-started/download/), and keep that shell with
  `VERSION` and `ARCH` set.
- Have the reviewed output from [Generate installation files](/docs/get-started/plan-values/)
  and complete [Prepare OpenBao](/docs/get-started/openbao/).
- Obtain the OpenBao CA bundle as `ca.crt` and the provider host JWT as
  `identity.jwt` from your identity provisioning process. The JWT must be
  renewable without the protected API server.

If your values use `auth.jwt.source: oauth2`, stage the client secret and
issuer CA bundle instead of `identity.jwt` in the steps below; see
[OAuth 2.0 client credentials](/docs/configure/oauth2/). The service unit
supports both sources.

## Step 1: Install the package or tarball

On Debian or Ubuntu:

```sh
sudo dpkg -i "bao-kms-provider_${VERSION}_linux_${ARCH}.deb"
```

On RHEL-family hosts:

```sh
sudo rpm -Uvh "bao-kms-provider_${VERSION}_linux_${ARCH}.rpm"
```

On other hosts, install the systemd tarball. The commands need GNU `install`,
`systemd-sysusers`, and `systemd-tmpfiles`; hosts without them must create the
same layout through configuration management, as described in
[Security: Linux identity model](/docs/security/linux-identity-model/).
Extract the archive and enter its versioned directory:

<!-- systemd-bundle-extract -->
```sh
tar -xzf "bao-kms-provider_${VERSION}_systemd_linux_${ARCH}.tar.gz"
cd "bao-kms-provider_${VERSION}_systemd_linux_${ARCH}"
```

Run the following block in a root shell from the extracted directory. It
installs the files and creates the service identities and directories. It
leaves live configuration and authentication material unchanged.

<!-- systemd-bundle-install -->
```sh
set -eu
install -D -o root -g root -m 0755 bin/bao-kms-provider /usr/bin/bao-kms-provider
install -D -o root -g root -m 0644 systemd/bao-kms-provider.service /usr/lib/systemd/system/bao-kms-provider.service
install -D -o root -g root -m 0644 sysusers.d/openbao-kms.conf /usr/lib/sysusers.d/openbao-kms.conf
install -D -o root -g root -m 0644 tmpfiles.d/openbao-kms.conf /usr/lib/tmpfiles.d/openbao-kms.conf
install -D -o root -g root -m 0644 config/provider-systemd.yaml /usr/share/doc/bao-kms-provider/examples/provider-systemd.yaml
install -D -o root -g root -m 0644 kubernetes/encryption-config.yaml /usr/share/doc/bao-kms-provider/examples/encryption-config.yaml
install -D -o root -g root -m 0644 README.md /usr/share/doc/bao-kms-provider/README.md
install -D -o root -g root -m 0644 LICENSE /usr/share/doc/bao-kms-provider/LICENSE
systemd-sysusers /usr/lib/sysusers.d/openbao-kms.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/openbao-kms.conf
```

Reload systemd to register the unit:

```sh
systemctl daemon-reload
```

The package and tarball leave the service disabled and stopped.

## Step 2: Copy the generated files

Copy the reviewed `generated/` directory from
[Generate installation files](/docs/get-started/plan-values/) to the node, next
to `ca.crt` and the credential. Read `generated/node-setup.sh` before you run
it: it is plain `sh`, and every path, owner, and mode comes from
`generated/config.yaml`.

Run it without arguments to print its phases and the identity fingerprint:

```sh
sh generated/node-setup.sh
```

Compare the fingerprint with `generated/installation.json` and your recorded
cluster identity. Every node must print the same fingerprint.

## Step 3: Run the node setup phases

Run each phase as root, in order. Each phase prints what it changed and the
command for the next one, and stops with a non-zero exit status when something
is wrong.

```sh
sudo sh generated/node-setup.sh prepare
sudo sh generated/node-setup.sh install --ca ca.crt --credential identity.jwt
sudo sh generated/node-setup.sh check
sudo sh generated/node-setup.sh start
```

| Phase | What it does |
|---|---|
| `prepare` | Checks the host tools, the package files, the `openbao-kms` user, and the socket group. Creates any missing credential or CA directory. |
| `install` | Installs `config.yaml`, the CA bundle, and the credential with the owner and mode the service user needs. Stops if any of them already exists. |
| `check` | Runs `config`, `verify-key`, and `doctor` as the `openbao-kms` user and confirms the fingerprint. |
| `start` | Enables and starts `bao-kms-provider.service`, then waits for HTTP 200 from `/ready`. Runs only after a passed `check` within the last hour. |

For native OAuth, pass the client secret as `--credential`, and the issuer CA
bundle as `--issuer-ca` when the configuration sets
`auth.jwt.oauth2.caCertFile`; see
[OAuth 2.0 client credentials](/docs/configure/oauth2/).

`install` refuses a node that already has a deployment. For an existing node,
follow [Operate: Upgrade](/docs/operate/upgrade/) instead and keep its identity
and state.

`check` exits with status `4` when `verify-key` or `doctor` reports a `[fail]`
check. Failures on a new setup are usually policy or auth problems; see
[Operate: Troubleshooting](/docs/operate/troubleshooting/) and
[Reference: CLI](/docs/reference/cli/#doctor). Read any `[warn]` lines before
you run `start`.

After the local API server starts using the socket, check its access with the
API server's own identity:

```sh
sudo sh /usr/share/bao-kms-provider/probe-apiserver
```

For a tarball installation, use `sudo sh bin/probe-apiserver` from the extracted
kit. The helper requires `pgrep`, `awk`, `setpriv`, and exactly one local running
`kube-apiserver`. Status, Encrypt, and Decrypt must pass. A root API server
produces a warning because the check does not prove non-root access.

If `start` times out waiting for `/ready`, check `journalctl -u
bao-kms-provider.service` for these common first-start causes:

- host DNS is not ready when the service starts,
- the OpenBao TLS server name does not match the certificate,
- the credential is expired or has the wrong audience or subject.

Continue to [Enable encryption](/docs/get-started/enable-encryption/) once the
provider is ready on every control-plane node.

## About the unit

The installed unit comes from `deploy/systemd/bao-kms-provider.service`. The
settings that matter for the control-plane boot path:

| Setting | Purpose |
|---|---|
| `Before=kubelet.service` | Orders process execution before kubelet when both units start together. It does not require kubelet to start this unit. |
| `Type=exec` | Completes startup ordering after successful execution of the provider binary. It does not wait for the KMS socket or `/ready`. |
| `ConditionPathExists=` | Skips a start attempt when the configuration is absent; start the unit after staging it. The provider validates the selected authentication source during startup. |
| `ConditionPathIsDirectory=/run/openbao-kms` | Requires the socket directory that tmpfiles creates. |
| `User=openbao-kms`, `SupplementaryGroups=openbao-kms-socket` | Runs without root; the socket group is how the API server connects. |
| `Restart=always` with start limits | Restarts transient failures without hiding a fast crash loop. |
| `ProtectSystem=strict`, `ReadWritePaths=/run/openbao-kms /var/lib/openbao-kms/state` | Makes the host read-only except the socket directory and non-secret registry state. |
| `CapabilityBoundingSet=`, `NoNewPrivileges=true` | Runs without Linux capabilities or privilege escalation. |

Enable the provider on every control-plane host. A missing, disabled, failed,
or unready provider does not prevent kubelet from starting. The API server
must retry its KMS connection during bootstrap; the unit does not gate or stop
kubelet on readiness. Before a planned API server restart, check `/ready`.

`network-online.target` orders the start but does not prove DNS, routing, or
OpenBao are reachable. The provider retries its initial checks for
`bootstrap.graceTimeout` before it exits. For the full hardening surface, see
[Security: Hardening](/docs/security/hardening/).
