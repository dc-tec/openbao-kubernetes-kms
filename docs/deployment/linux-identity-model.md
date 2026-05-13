---
title: "Linux Identity Model"
description: "User, group, file ownership, and runtime directory creation model for running bao-kms-provider in systemd or static pod mode."
weight: 40
---

# Linux Identity Model

This page captures the user, group, file ownership, and runtime directory model shared by systemd and static-pod deployments. The API server must be able to connect to the provider socket. It must not gain access to provider auth material or writable provider state through that socket access path.

## Goals

- Run the provider as a non-root user.
- Let the provider read its configuration and selected auth material.
- Let `kube-apiserver` connect to the Unix socket.
- Avoid giving `kube-apiserver` access to provider auth material.
- Avoid making the provider primary group equal to the `kube-apiserver` group.
- Keep the model workable for kubeadm static-pod API servers and host-service API servers.

## Selected Model

```text
user:         openbao-kms
group:        openbao-kms
socket group: openbao-kms-socket
```

Package installs create these identities through `sysusers.d` where available. Host images without `sysusers.d` support should create equivalent system users and groups during image build or configuration management.

Permissions:

```text
/etc/openbao-kms/config.yaml        root:openbao-kms                0640
/etc/openbao-kms/tls/ca.crt         root:root                       0644
/var/lib/openbao-kms                openbao-kms:openbao-kms         0750
/var/lib/openbao-kms/identity.jwt   root:openbao-kms                0640
/etc/openbao-kms/client/client-chain.pem root:openbao-kms           0640
/etc/openbao-kms/pkcs11/pin         root:openbao-kms                0640
/var/lib/openbao-kms/state          openbao-kms:openbao-kms         0750
/run/openbao-kms                    openbao-kms:openbao-kms-socket  2750
/run/openbao-kms/kms.sock           openbao-kms:openbao-kms-socket  0660
```

Access matrix:

| Actor | Required access | Must not have |
|---|---|---|
| `bao-kms-provider` process | read config, CA, selected auth material; write local registry state; create and own `kms.sock` | broad host write access or Linux capabilities |
| `kube-apiserver` process | connect to `/run/openbao-kms/kms.sock` | read access to provider auth material |
| OpenBao administrator | manage Transit key, policy, and provider auth | access to Kubernetes etcd plaintext through this model |
| package manager or host automation | create users, groups, directories, unit files, and examples | runtime access to provider token material after rollout |

systemd service:

```ini
User=openbao-kms
Group=openbao-kms
SupplementaryGroups=openbao-kms-socket
```

The local `kube-apiserver` identity should be allowed to connect through the socket group. On hosts where `kube-apiserver` runs as root, root can connect regardless. The group model still provides a non-root packaging path.

Static pod mode uses the numeric host GID for `openbao-kms-socket` in both:

- `spec.securityContext.supplementalGroups`,
- `server.socketGroup` in provider configuration.

This avoids depending on host group names being present inside the distroless non-root image.

For static pods, use the numeric GID from the host:

```sh
getent group openbao-kms-socket
```

The third field in the output is the value used in both the pod manifest and static-pod provider configuration.

## Runtime Directory Creation

`RuntimeDirectory=` alone may create `/run/openbao-kms` with the service primary group rather than the socket access group. Packaging should prefer one of:

- a `tmpfiles.d` entry that creates `/run/openbao-kms` with `openbao-kms:openbao-kms-socket` and mode `2750`,
- a privileged package install step that creates the directory before service start,
- a root pre-start helper that only creates and `chown`s the runtime directory.

The provider validates the directory at startup and fails closed if it is unsafe.

The mode `2750` is intentional:

- owner `openbao-kms` can create and remove the socket,
- group `openbao-kms-socket` can traverse the directory,
- the setgid bit keeps the socket group stable,
- the group cannot replace arbitrary files in the directory because group write is absent,
- world access is absent.

The socket itself is `0660`, so members of `openbao-kms-socket` can connect to the provider without receiving access to auth material or registry state.

## Tradeoffs

### Separate Socket Group

Pros:

- `kube-apiserver` gets socket access without auth-material access,
- `kube-apiserver` can connect without being able to replace the socket path,
- the provider keeps a private primary group,
- the model works with non-root `kube-apiserver` services,
- the privilege boundary is explicit.

Cons:

- packaging must create an additional group,
- `kube-apiserver` group membership varies by distribution,
- static pod deployments need host group mapping or root access.

### Primary Group Equals kube-apiserver Group

Pros:

- simpler socket access,
- fewer groups to create.

Cons:

- easier to accidentally expose provider-readable files to the API server group,
- weaker privilege separation,
- distribution-specific `kube-apiserver` group naming leaks into provider packaging.

### Root-Owned Socket Directory

Pros:

- straightforward for kubeadm static-pod API servers running as root,
- avoids `kube-apiserver` group detection.

Cons:

- weaker non-root story,
- less portable to hardened API server services,
- can hide permission problems until deployment hardening.

## Decision

The separate socket group is the default packaging model. Distribution packaging may choose different names, but it must preserve the same privilege split: provider auth-material access is separate from `kube-apiserver` socket access.
