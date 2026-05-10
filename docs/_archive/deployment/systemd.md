# systemd Deployment

systemd is the preferred hardened deployment model when operators control the host operating system.

## Why systemd

systemd avoids depending on kubelet, the container runtime, or the Kubernetes API server to start the KMS plugin. That matters because kube-apiserver may require the KMS plugin to decrypt already-encrypted resources during startup.

## Recommended Unit

The maintained sample unit lives at [`deploy/systemd/bao-kms-provider.service`](../../deploy/systemd/bao-kms-provider.service). It uses the resolved identity model from [Linux identity model](linux-identity-model.md).

```ini
[Unit]
Description=OpenBao Kubernetes KMS v2 Provider
Documentation=https://github.com/dc-tec/openbao-kubernetes-kms
Wants=network-online.target
After=network-online.target
Before=kubelet.service
ConditionPathExists=/etc/openbao-kms/config.yaml
ConditionPathExists=/var/lib/openbao-kms/identity.jwt
ConditionPathIsDirectory=/run/openbao-kms

[Service]
Type=exec
User=openbao-kms
Group=openbao-kms
SupplementaryGroups=openbao-kms-socket
ExecStart=/usr/bin/bao-kms-provider serve --config /etc/openbao-kms/config.yaml
Restart=always
RestartSec=5s
StartLimitIntervalSec=60
StartLimitBurst=10
UMask=0027

NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
RestrictRealtime=true
SystemCallArchitectures=native
ReadWritePaths=/run/openbao-kms /var/lib/openbao-kms/state
ReadOnlyPaths=/etc/openbao-kms
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
LockPersonality=true
MemoryDenyWriteExecute=true
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
```

The exact ordering depends on the Kubernetes distribution. For kubeadm-style hosts, the goal is that the plugin socket is available before kubelet starts the static pod API server.

Use [`deploy/config/provider-systemd.yaml`](../../deploy/config/provider-systemd.yaml) as the starting provider config for host-service deployments.

## Directory Setup

Example:

```sh
install -d -o root -g root -m 0750 /etc/openbao-kms
install -d -o root -g root -m 0755 /etc/openbao-kms/tls
install -d -o openbao-kms -g openbao-kms -m 0750 /var/lib/openbao-kms
install -d -o openbao-kms -g openbao-kms -m 0750 /var/lib/openbao-kms/state
install -d -o openbao-kms -g openbao-kms-socket -m 2750 /run/openbao-kms
```

The service should verify `/run/openbao-kms` at startup. Packaging should create it through `tmpfiles.d` or an equivalent root-owned install step so the group is `openbao-kms-socket` and the setgid bit preserves the socket access group. The socket access group needs execute permission on the directory and write permission on `kms.sock`; it must not have write permission on the directory itself.

Sample `tmpfiles.d` entries live under [`deploy/package/linux/tmpfiles.d/openbao-kms.conf`](../../deploy/package/linux/tmpfiles.d/openbao-kms.conf). The runtime-only entry is:

```text
d /run/openbao-kms 2750 openbao-kms openbao-kms-socket -
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

The provider retries the initial status probe for `bootstrap.graceTimeout` before exiting. Keep this grace long enough for JWT projection, DNS/routing, OpenBao restart, and clock-sync races, but short enough that deterministic misconfiguration is visible in service status.

Use [Troubleshooting](../troubleshooting.md) for recovery.
