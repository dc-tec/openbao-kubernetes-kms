---
title: "systemd Deployment"
description: "Hardened systemd unit, directory setup, and startup procedure for running bao-kms-provider as a host service."
weight: 20
---

# systemd Deployment

systemd is the preferred hardened deployment model when operators control the host operating system. It avoids depending on kubelet, the container runtime, or the Kubernetes API server to start the KMS plugin, which matters because `kube-apiserver` may require the plugin to decrypt already-encrypted resources during startup.

This page covers the unit file, directory setup, and startup procedure. For the model selection rationale see [Deployment: Choosing A Model](/deployment/choosing-a-model/). For the user, group, and file ownership model see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

## Recommended Unit

The maintained sample unit lives at `deploy/systemd/bao-kms-provider.service` in the repository. It uses the identity model from [Linux Identity Model](/deployment/linux-identity-model/).

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
ExecStart=/usr/local/bin/bao-kms-provider serve --config /etc/openbao-kms/config.yaml
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

The exact ordering depends on the Kubernetes distribution. For kubeadm-style hosts, the goal is that the plugin socket is available before kubelet starts the static-pod API server.

Use `deploy/config/provider-systemd.yaml` as the starting provider configuration for host-service deployments.

## Directory Setup

```sh
install -d -o root -g root -m 0750 /etc/openbao-kms
install -d -o root -g root -m 0755 /etc/openbao-kms/tls
install -d -o openbao-kms -g openbao-kms -m 0750 /var/lib/openbao-kms
install -d -o openbao-kms -g openbao-kms -m 0750 /var/lib/openbao-kms/state
install -d -o openbao-kms -g openbao-kms-socket -m 2750 /run/openbao-kms
```

The service verifies `/run/openbao-kms` at startup. Packaging should create the runtime directory through `tmpfiles.d` or an equivalent root-owned install step so the group is `openbao-kms-socket` and the setgid bit preserves the socket access group. The socket access group needs execute permission on the directory and write permission on `kms.sock`; it must not have write permission on the directory itself.

A sample `tmpfiles.d` entry lives under `deploy/package/linux/tmpfiles.d/openbao-kms.conf`. The runtime-only entry is:

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
- Keep the JWT readable only by the plugin process.
- Keep the socket writable only by the plugin and the API server identity.
- Verify `ProtectSystem=strict` does not block required paths.
- Bind metrics and health endpoints to localhost unless explicitly needed.
- Avoid debug endpoints.
- Use systemd restart limits suitable for control-plane recovery.

For the broader hardening surface beyond the systemd unit see [Security: Hardening](/security/hardening/).

## Failure Modes

Common failures during initial bring-up:

- the service starts after kubelet or the API server,
- the socket directory group is wrong,
- `ProtectSystem` blocks the configuration file or JWT file,
- the CA bundle path is missing,
- host DNS is not ready before service start,
- the OpenBao TLS server name does not match the certificate.

The provider retries the initial status probe for `bootstrap.graceTimeout` before exiting. Keep the grace long enough for JWT projection, DNS or routing, OpenBao restart, and clock-sync races. Keep it short enough that deterministic misconfiguration is visible in service status.

For diagnosis and recovery see [Operations: Troubleshooting](/operations/troubleshooting/). For provider upgrade procedure see [Operations: Upgrade](/operations/upgrade/).
