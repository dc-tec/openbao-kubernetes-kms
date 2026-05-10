---
title: "Linux Identity Model"
description: "User, group, file ownership, and runtime directory creation model for running bao-kms-provider in systemd or static pod mode."
weight: 40
---

# Linux Identity Model

This page captures the user, group, and file ownership model that both supported deployment styles use. The model is shared between systemd and static-pod deployments because the API server must be able to connect to the socket without being able to read the provider JWT.

## Goals

- Run the provider as a non-root user.
- Let the provider read its configuration and JWT.
- Let `kube-apiserver` connect to the Unix socket.
- Avoid giving `kube-apiserver` access to the provider JWT.
- Avoid making the provider primary group equal to the `kube-apiserver` group.
- Keep the model workable for kubeadm static-pod API servers and host-service API servers.

## Selected Model

```text
user:         openbao-kms
group:        openbao-kms
socket group: openbao-kms-socket
```

Permissions:

```text
/etc/openbao-kms/config.yaml        root:openbao-kms                0640
/etc/openbao-kms/tls/ca.crt         root:root                       0644
/var/lib/openbao-kms                openbao-kms:openbao-kms         0750
/var/lib/openbao-kms/identity.jwt   root:openbao-kms                0640
/var/lib/openbao-kms/state          openbao-kms:openbao-kms         0750
/run/openbao-kms                    openbao-kms:openbao-kms-socket  2750
/run/openbao-kms/kms.sock           openbao-kms:openbao-kms-socket  0660
```

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

## Runtime Directory Creation

`RuntimeDirectory=` alone may create `/run/openbao-kms` with the service primary group rather than the socket access group. Packaging should prefer one of:

- a `tmpfiles.d` entry that creates `/run/openbao-kms` with `openbao-kms:openbao-kms-socket` and mode `2750`,
- a privileged package install step that creates the directory before service start,
- a root pre-start helper that only creates and `chown`s the runtime directory.

The provider validates the directory at startup and fails closed if it is unsafe.

## Tradeoffs

### Separate Socket Group

Pros:

- `kube-apiserver` gets socket access without JWT access,
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

The separate socket group is the default packaging model. Distribution packaging may choose different names, but it must preserve the same privilege split: provider JWT access is separate from `kube-apiserver` socket access.
