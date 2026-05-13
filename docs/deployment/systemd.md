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
StartLimitIntervalSec=60
StartLimitBurst=10
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
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SystemCallArchitectures=native
LockPersonality=true
MemoryDenyWriteExecute=true
CapabilityBoundingSet=
AmbientCapabilities=
ReadOnlyPaths=/etc/openbao-kms
ReadWritePaths=/run/openbao-kms /var/lib/openbao-kms/state

[Install]
WantedBy=multi-user.target
```

The exact ordering depends on the Kubernetes distribution. For kubeadm-style hosts, the goal is that the plugin socket is available before kubelet starts the static-pod API server.

Use `deploy/config/provider-systemd.yaml` as the starting provider configuration for host-service deployments.

The sample unit is written for the default JWT config. Certificate auth deployments should replace the JWT `ConditionPathExists=` line with checks for the configured certificate chain, PKCS#11 PIN file, or SPIFFE Workload API socket.

## Unit Settings

| Setting | Purpose |
|---|---|
| `Before=kubelet.service` | Starts the provider before kubelet starts static-pod control-plane components on kubeadm-style hosts. |
| `ConditionPathExists=` | Fails early when config or selected auth material has not been staged. |
| `ConditionPathIsDirectory=` | Requires the runtime socket directory to exist with packaging-controlled ownership. |
| `Type=exec` | Surfaces `execve` failures before systemd marks the service started. |
| `Restart=always` and restart limits | Restarts transient provider failures without hiding a fast crash loop. |
| `UMask=0027` | Prevents permissive files created by the process. |
| `NoNewPrivileges=true` | Blocks privilege escalation through setuid or file capabilities. |
| `ProtectSystem=strict` | Makes the host filesystem read-only except explicitly allowed paths. |
| `ReadOnlyPaths=/etc/openbao-kms` | Allows config and CA reads without making the directory writable. |
| `ReadWritePaths=/run/openbao-kms /var/lib/openbao-kms/state` | Limits writes to the socket directory and non-secret local registry state. |
| `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` | Allows Unix sockets plus IPv4 and IPv6 OpenBao traffic. |
| `CapabilityBoundingSet=` and `AmbientCapabilities=` | Runs without Linux capabilities. |

`network-online.target` is only an ordering hint. It does not prove DNS, routing, OpenBao TLS, or the OpenBao load balancer is ready. The provider's `bootstrap.graceTimeout` handles those boot races by retrying the initial status probe before exiting.

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

After the Kubernetes `EncryptionConfiguration` is staged, include it in the check:

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/encryption-config.yaml
```

## Hardening Checklist

- Run as non-root where possible.
- Keep auth material readable only by the plugin process.
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
- `ProtectSystem` blocks the configuration file or auth material,
- the CA bundle path is missing,
- host DNS is not ready before service start,
- the OpenBao TLS server name does not match the certificate.

The provider retries the initial status probe for `bootstrap.graceTimeout` before exiting. Keep the grace long enough for auth material projection, DNS or routing, OpenBao restart, and clock-sync races. Keep it short enough that deterministic misconfiguration is visible in service status.

For diagnosis and recovery see [Operations: Troubleshooting](/operations/troubleshooting/). For provider upgrade procedure see [Operations: Upgrade](/operations/upgrade/).
