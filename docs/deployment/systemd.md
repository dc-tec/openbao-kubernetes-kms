# systemd Deployment

systemd is the preferred hardened deployment model when operators control the host operating system.

## Why systemd

systemd avoids depending on kubelet, the container runtime, or the Kubernetes API server to start the KMS plugin. That matters because kube-apiserver may require the KMS plugin to decrypt already-encrypted resources during startup.

## Recommended Unit

Candidate unit. The Linux identity model is still under discussion; see [Linux identity model](linux-identity-model.md).

```ini
[Unit]
Description=OpenBao Kubernetes KMS Provider
Documentation=https://github.com/openbao/openbao
Wants=network-online.target
After=network-online.target
Before=kubelet.service

[Service]
Type=simple
User=openbao-kms
Group=openbao-kms
SupplementaryGroups=openbao-kms-socket
ExecStart=/usr/local/bin/bao-kms-provider serve --config /etc/openbao-kms/config.yaml
Restart=always
RestartSec=2s

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/run/openbao-kms /var/lib/openbao-kms/state
ReadOnlyPaths=/etc/openbao-kms
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
LockPersonality=true
MemoryDenyWriteExecute=true

[Install]
WantedBy=multi-user.target
```

The exact ordering depends on the Kubernetes distribution. For kubeadm-style hosts, the goal is that the plugin socket is available before kubelet starts the static pod API server.

## Directory Setup

Example:

```sh
install -d -o root -g root -m 0750 /etc/openbao-kms
install -d -o root -g root -m 0755 /etc/openbao-kms/tls
install -d -o openbao-kms -g openbao-kms -m 0750 /var/lib/openbao-kms
install -d -o openbao-kms -g openbao-kms -m 0750 /var/lib/openbao-kms/state
install -d -o openbao-kms -g openbao-kms-socket -m 2770 /run/openbao-kms
```

The service should verify `/run/openbao-kms` at startup. Packaging should create it through `tmpfiles.d` or an equivalent root-owned install step so the group is `openbao-kms-socket` and the setgid bit preserves the socket access group.

Example `tmpfiles.d` entry:

```text
d /run/openbao-kms 2770 openbao-kms openbao-kms-socket -
```

## Start

```sh
systemctl daemon-reload
systemctl enable bao-kms-provider.service
systemctl start bao-kms-provider.service
systemctl status bao-kms-provider.service
```

Run `doctor` before enabling kube-apiserver encryption:

```sh
bao-kms-provider doctor --config /etc/openbao-kms/config.yaml
```

## Hardening Checklist

- Run as non-root where possible.
- Keep JWT readable only by the plugin.
- Keep socket writable only by plugin and kube-apiserver.
- Verify `ProtectSystem=strict` does not block required paths.
- Bind metrics and health endpoints to localhost unless explicitly needed.
- Avoid debug endpoints.
- Use systemd restart limits suitable for control-plane recovery.

## Upgrade

Upgrade one control-plane node at a time:

1. Run `doctor` with the new binary.
2. Stop service on one node.
3. Replace binary.
4. Start service.
5. Verify `/ready`.
6. Verify KMS Status key hash matches the cluster.
7. Continue to the next node.

## Failure Modes

Common failures:

- service starts after kubelet/API server,
- socket directory group is wrong,
- `ProtectSystem` blocks config or JWT file,
- CA bundle path is missing,
- host DNS is not ready before service start,
- OpenBao TLS server name mismatch.

Use [Troubleshooting](../troubleshooting.md) for recovery.
