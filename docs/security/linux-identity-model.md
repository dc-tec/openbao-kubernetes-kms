---
title: Linux identity model
description: "The users, groups, file ownership, and socket directory rules that let the API server reach the provider without reading its credentials."
eyebrow: Security · Host
weight: 35
verifiedBy:
  - deploy/package/linux/sysusers.d/openbao-kms.conf
  - deploy/package/linux/tmpfiles.d/openbao-kms.conf
  - deploy/systemd/bao-kms-provider.service
  - internal/socket
---

The provider runs as a non-root user with its own primary group. The API server
reaches the socket through a separate group that grants no access to the
provider's auth material or state. systemd and static-pod deployments use the
same model.

## Identities

```text
user:         openbao-kms
group:        openbao-kms
socket group: openbao-kms-socket
```

Packages create these through `sysusers.d`. Hosts without `sysusers.d` must
create equivalent system users and groups through their image build or
configuration management.

## File ownership

```text
/etc/openbao-kms                         root:openbao-kms                0750
/etc/openbao-kms/tls                     root:root                       0755
/etc/openbao-kms/config.yaml             root:openbao-kms                0640
/etc/openbao-kms/tls/ca.crt              root:root                       0644
/var/lib/openbao-kms                     openbao-kms:openbao-kms         0750
/var/lib/openbao-kms/identity.jwt        root:openbao-kms                0640
/etc/openbao-kms/client/client-chain.pem root:openbao-kms                0640
/etc/openbao-kms/pkcs11/pin              root:openbao-kms                0640
/var/lib/openbao-kms/state               openbao-kms:openbao-kms         0750
/run/openbao-kms                         openbao-kms:openbao-kms-socket  2750
/run/openbao-kms/kms.sock                openbao-kms:openbao-kms-socket  0660
```

Static pods use the numeric container user `65532` in place of `openbao-kms`,
and the numeric socket GID for the runtime directory:

```text
/etc/openbao-kms                                   root:65532           0750
/etc/openbao-kms/tls                               root:root            0755
/etc/openbao-kms/config.yaml                       root:65532           0640
/etc/openbao-kms/tls/ca.crt                        root:root            0644
/var/lib/openbao-kms/credentials                   root:65532           0750
/var/lib/openbao-kms/credentials/identity.jwt      root:65532           0640
/var/lib/openbao-kms/state                         65532:65532          0750
/run/openbao-kms                                   65532:<socket GID>   2750
```

The generated `node-setup.sh` creates this layout from the resolved
configuration; see [Run as a static pod](/docs/get-started/static-pod/#step-3-run-the-node-setup-phases).
Use these tables when you manage the host through your own automation.

| Actor | Needs | Must not have |
|---|---|---|
| Provider | Read configuration, CA bundle, and auth material; write registry state; own `kms.sock` | Broad host write access or Linux capabilities |
| `kube-apiserver` | Connect to `/run/openbao-kms/kms.sock` | Read access to provider auth material |
| OpenBao administrator | Manage the Transit key, policy, and auth | Kubernetes plaintext through this model |
| Package manager or host automation | Create users, groups, directories, units, and examples | Access to provider tokens after rollout |

## Socket access

The systemd unit adds `SupplementaryGroups=openbao-kms-socket`, and the
API server's user must be a member of that group. An API server that runs as
root connects regardless.

Static pods cannot resolve host group names inside the distroless image. Put
the numeric GID from `getent group openbao-kms-socket` in both
`spec.securityContext.supplementalGroups` and `server.socketGroup`.

## Runtime directory

Create `/run/openbao-kms` with a `tmpfiles.d` entry, a privileged install step,
or a root pre-start helper. `RuntimeDirectory=` alone can assign the wrong
group. The provider checks the directory at startup and fails closed if it is
unsafe. The directory must be owned by the provider's effective UID, must not
be a symlink, and must not allow group or world write. A non-root provider
therefore cannot use a root-owned runtime directory. Set its owner during
installation, before starting the provider.

Mode `2750` lets the owner create and remove the socket and lets the socket
group traverse the directory. The setgid bit keeps the socket group stable, and
the missing group write stops the group from replacing entries. The socket
itself is `0660`.

## Why a separate socket group

| Option | Benefit | Cost |
|---|---|---|
| Separate socket group (selected) | Socket access without credential access; works with non-root API servers; explicit privilege boundary. | One more group; API server group membership differs per distribution; static pods need the numeric GID. |
| Provider primary group equals API server group | Fewer groups. | Easy to expose provider files to the API server; distribution-specific group names leak into packaging. |
| Root-owned socket directory | Simple for kubeadm API servers that run as root. | No non-root story; hides permission problems until hardening. |

Distribution packages can choose other names but must keep the same split
between credential access and socket access.
