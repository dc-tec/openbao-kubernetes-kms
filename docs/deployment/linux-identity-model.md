# Linux Identity Model

This document captures the systemd packaging user/group decision that still needs final confirmation.

## Goals

- Run the provider as a non-root user.
- Let the provider read its config and JWT.
- Let kube-apiserver connect to the Unix socket.
- Avoid giving kube-apiserver access to the provider JWT.
- Avoid making the provider primary group equal to the kube-apiserver group.
- Keep the model workable for kubeadm static-pod API servers and host-service API servers.

## Candidate Model

Recommended candidate:

```text
user:  openbao-kms
group: openbao-kms
socket group: openbao-kms-socket
```

Permissions:

```text
/etc/openbao-kms/config.yaml        root:openbao-kms 0640
/etc/openbao-kms/tls/ca.crt         root:root 0644
/var/lib/openbao-kms                openbao-kms:openbao-kms 0750
/var/lib/openbao-kms/identity.jwt   root:openbao-kms 0640
/var/lib/openbao-kms/state          openbao-kms:openbao-kms 0750
/run/openbao-kms                    openbao-kms:openbao-kms-socket 2770
/run/openbao-kms/kms.sock           openbao-kms:openbao-kms-socket 0660
```

systemd service:

```ini
User=openbao-kms
Group=openbao-kms
SupplementaryGroups=openbao-kms-socket
```

The local kube-apiserver identity should be allowed to connect through the socket group. On hosts where kube-apiserver runs as root, root can connect regardless, but the group model still provides a non-root packaging path.

## Runtime Directory Creation

`RuntimeDirectory=` alone may create `/run/openbao-kms` with the service primary group rather than the socket access group. Packaging should prefer one of:

- a `tmpfiles.d` entry that creates `/run/openbao-kms` with `openbao-kms:openbao-kms-socket` and mode `2770`,
- a privileged package install step that creates the directory before service start,
- a root pre-start helper that only creates/chowns the runtime directory.

The provider should still validate the directory at startup and fail closed if it is unsafe.

## Tradeoffs

### Separate Socket Group

Pros:

- kube-apiserver gets socket access without JWT access,
- provider keeps a private primary group,
- works with non-root kube-apiserver services,
- clear least-privilege boundary.

Cons:

- package must create an additional group,
- kube-apiserver group membership varies by distribution,
- static pod deployments need host group mapping or root access.

### Primary Group Equals kube-apiserver Group

Pros:

- simpler socket access,
- fewer groups to create.

Cons:

- easier to accidentally expose provider-readable files to the API server group,
- less clear privilege separation,
- distribution-specific kube-apiserver group naming leaks into provider packaging.

### Root-Owned Socket Directory

Pros:

- simple for kubeadm static-pod API servers running as root,
- avoids kube-apiserver group detection.

Cons:

- weak non-root story,
- less portable to hardened API server services,
- can hide permission problems until deployment hardening.

## Open Decision

Default to the separate socket group unless implementation or distro packaging tests show it is impractical.

This decision should become an ADR before packaging artifacts are considered stable.

