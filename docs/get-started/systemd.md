---
title: Run with systemd
description: "Install the provider as a hardened systemd service on each control-plane node, configure it, validate it with doctor, and start it."
eyebrow: Get started · Step 6
weight: 60
verifiedBy:
  - deploy/systemd/bao-kms-provider.service
  - deploy/package/linux
  - deploy/config/provider-systemd.yaml
  - test/deployment/systemd-install.sh
---

Repeat this procedure on every control-plane node. At the end, the provider
runs as the non-root `openbao-kms` user, listens on
`/run/openbao-kms/kms.sock`, and has passed `doctor` against OpenBao.

## Before you begin

- Download and verify the artifact in
  [Download the release](/docs/get-started/download/), and keep that shell with
  `VERSION` and `ARCH` set.
- Have the values from [Plan identity values](/docs/get-started/plan-values/)
  and the lineage ID from [Prepare OpenBao](/docs/get-started/openbao/).
- Obtain the OpenBao CA bundle as `ca.crt` and the provider host JWT as
  `identity.jwt` from your identity provisioning process. The JWT must be
  renewable without the protected API server.

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

## Step 2: Correct directory access on preview releases

The packages and tarballs for `0.1.0-preview.1` and `0.1.0-preview.2` create
`/etc/openbao-kms` with group `root`, so the service user cannot read its
configuration. On these releases, run this block as root. It adds a persistent
tmpfiles override so the fix survives reboot. If an override already exists,
update its `/etc/openbao-kms` entry by hand instead.

<!-- systemd-preview-permissions -->
```sh
set -eu
test ! -e /etc/tmpfiles.d/openbao-kms.conf
install -d -o root -g root -m 0755 /etc/tmpfiles.d
sed 's@^d /etc/openbao-kms 0750 root root -$@d /etc/openbao-kms 0750 root openbao-kms -@' \
  /usr/lib/tmpfiles.d/openbao-kms.conf > /etc/tmpfiles.d/openbao-kms.conf
chmod 0644 /etc/tmpfiles.d/openbao-kms.conf
systemd-tmpfiles --create /etc/tmpfiles.d/openbao-kms.conf
```

Remove the override once an installed release sets the group to `openbao-kms`
in `/usr/lib/tmpfiles.d/openbao-kms.conf`, unless it holds other local changes.

## Step 3: Write the provider configuration

Copy the installed example to a working file:

```sh
cp /usr/share/doc/bao-kms-provider/examples/provider-systemd.yaml provider.yaml
```

Edit `provider.yaml` and replace the sample values in the fields listed in
[Plan identity values: Provider configuration](/docs/get-started/plan-values/#provider-configuration)
with your recorded values.

Keep the other fields at their sample values unless you have a reason to change
them; see [Reference: Configuration](/docs/reference/configuration/).

## Step 4: Place the runtime files

From the directory that holds `provider.yaml`, `ca.crt`, and `identity.jwt`,
run as root:

<!-- systemd-runtime-files -->
```sh
set -eu
test ! -e /etc/openbao-kms/config.yaml
test ! -e /etc/openbao-kms/tls/ca.crt
test ! -e /var/lib/openbao-kms/identity.jwt
install -o root -g openbao-kms -m 0640 provider.yaml /etc/openbao-kms/config.yaml
install -o root -g root -m 0644 ca.crt /etc/openbao-kms/tls/ca.crt
install -o root -g openbao-kms -m 0640 identity.jwt /var/lib/openbao-kms/identity.jwt
```

The `test` lines stop the block on a node that already has a deployment. For
an existing node, follow [Operate: Upgrade](/docs/operate/upgrade/) instead and
keep its identity and state.

## Step 5: Validate as the service user

Run the checks as `openbao-kms`, so an unreadable file fails here the same way
it would fail in the service.

Resolve the configuration and print its identity fingerprint:

```sh
sudo -u openbao-kms bao-kms-provider config --config /etc/openbao-kms/config.yaml
```

Check the Transit key profile, then run the full bootstrap check against
OpenBao:

```sh
sudo -u openbao-kms bao-kms-provider verify-key --config /etc/openbao-kms/config.yaml
sudo -u openbao-kms bao-kms-provider doctor --config /etc/openbao-kms/config.yaml
```

Each command exits with status `0`, and neither `verify-key` nor `doctor`
reports a `[fail]` check. Every control-plane node must print the same
identity fingerprint. `doctor` failures on a new setup are usually policy or
auth problems; see [Operate: Troubleshooting](/docs/operate/troubleshooting/)
and [Reference: CLI](/docs/reference/cli/#doctor).

## Step 6: Start the service

```sh
systemctl enable --now bao-kms-provider.service
systemctl status bao-kms-provider.service
```

`systemctl status` reports the service as active, and
`/run/openbao-kms/kms.sock` exists. The provider is ready for
[Enable encryption](/docs/get-started/enable-encryption/) once it runs on every
control-plane node.

If the service does not become active, check these common first-start causes:

- the service starts after kubelet or the API server,
- the socket directory group is not `openbao-kms-socket`,
- `ProtectSystem` blocks a configuration or auth material path,
- the CA bundle path is missing,
- host DNS is not ready when the service starts,
- the OpenBao TLS server name does not match the certificate.

## About the unit

The installed unit comes from `deploy/systemd/bao-kms-provider.service`. The
settings that matter for the control-plane boot path:

| Setting | Purpose |
|---|---|
| `Before=kubelet.service` | Starts the provider before kubelet starts a static-pod API server on kubeadm-style hosts. |
| `ConditionPathExists=` | Skips start until the configuration and JWT are staged. PKCS#11 deployments replace the JWT condition with their certificate chain and PIN file. |
| `ConditionPathIsDirectory=/run/openbao-kms` | Requires the socket directory that tmpfiles creates. |
| `User=openbao-kms`, `SupplementaryGroups=openbao-kms-socket` | Runs without root; the socket group is how the API server connects. |
| `Restart=always` with start limits | Restarts transient failures without hiding a fast crash loop. |
| `ProtectSystem=strict`, `ReadWritePaths=/run/openbao-kms /var/lib/openbao-kms/state` | Makes the host read-only except the socket directory and non-secret registry state. |
| `CapabilityBoundingSet=`, `NoNewPrivileges=true` | Runs without Linux capabilities or privilege escalation. |

`network-online.target` orders the start but does not prove DNS, routing, or
OpenBao are reachable. The provider retries its initial checks for
`bootstrap.graceTimeout` before it exits. For the full hardening surface, see
[Security: Hardening](/docs/security/hardening/).
