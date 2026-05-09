# 0012: Deployment Identity And Container Image

## Status

Accepted

## Context

WS10 adds installable deployment artifacts for both systemd and static pod modes. The provider must run as non-root, keep the JWT private to the provider process, and expose a Unix socket that kube-apiserver can connect to.

Static pod mode adds an extra constraint: a distroless container should not depend on host group names being present inside the image. The socket group therefore needs to support numeric host GIDs.

## Decision

Use this Linux identity model for sample and package artifacts:

```text
user:         openbao-kms
group:        openbao-kms
socket group: openbao-kms-socket
```

The provider primary group remains separate from the socket group. kube-apiserver receives access to the socket group only; it does not need access to the provider JWT.

`server.socketGroup` supports either:

- a local group name, for systemd and host binary deployments,
- a decimal numeric GID, for static pod deployments where the container should not rely on host `/etc/group`.

Container images use a distroless non-root runtime base, run as `65532:65532`, and are pinned by digest in the Dockerfile and central version policy. The image contains only the statically linked `bao-kms-provider` binary.

## Consequences

- Package artifacts must create both `openbao-kms` and `openbao-kms-socket`.
- `/run/openbao-kms` must be created as `openbao-kms:openbao-kms-socket` with mode `2770`.
- Static pod manifests must set `runAsUser: 65532`, `runAsGroup: 65532`, and a host-specific `supplementalGroups` numeric GID that matches the host socket group.
- The static pod provider config should set `server.socketGroup` to the same numeric GID.
- Operators must replace sample image digests and sample GIDs with site-specific values before deployment.
