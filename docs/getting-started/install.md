---
title: "Install"
description: "Fetch a verified provider binary or container image, place the runtime files, and validate the local environment with doctor."
weight: 30
---

# Install

This page covers fetching a verified `bao-kms-provider` artifact, placing the runtime files, and confirming the local environment is sane before wiring the provider into Kubernetes. It assumes [OpenBao Setup](/getting-started/openbao-setup/) has been completed.

## Status

No public release exists for v0.1 yet. The repository ships sample build and deployment artifacts that release engineering will replace with published binaries and signed container images:

- `Dockerfile` for the `bao-kms-provider` container image,
- `deploy/config/provider-systemd.yaml` and `deploy/config/provider-static-pod.yaml` configuration samples,
- `deploy/systemd/bao-kms-provider.service` systemd unit,
- `deploy/static-pod/bao-kms-provider.yaml` static pod manifest,
- `deploy/kubernetes/encryption-config.yaml` API server `EncryptionConfiguration` sample,
- `deploy/package/linux` package layout snippets.

For local builds during development, see [Development: Contributing](/development/contributing/).

## Choose The Artifact

The provider ships in two forms.

| Form | Use | Details |
|---|---|---|
| Linux binary | systemd deployment | Tarball with checksum, signature, and SBOM in the release assets. |
| Container image | static-pod deployment | Distroless non-root image, runs as `65532:65532`, pinned to a base image digest in `.ci/versions.yaml`. |

The choice between systemd and static-pod is made on a separate page. See [Deployment: Choosing A Model](/deployment/choosing-a-model/) once the artifact is in place.

## Verify The Artifact

The release evidence for each stable release includes:

- a checksum file (`SHA256SUMS`),
- detached cryptographic signatures over the checksum file,
- an SBOM per binary and per image,
- a provenance attestation generated during the release workflow.

Verify in this order:

1. Fetch the artifact and the checksum file from the release page.
2. Compare the SHA-256 of the artifact against the entry in the checksum file.
3. Verify the signature over the checksum file using the public key documented in the release evidence.
4. Verify the provenance attestation against the expected workflow identity.

For the full evidence catalog and supply-chain controls behind these artifacts, see [Development: CI And Supply Chain](/development/ci-supply-chain/).

## Place The Files

Recommended host layout:

```text
/usr/local/bin/bao-kms-provider
/etc/openbao-kms/config.yaml
/etc/openbao-kms/tls/ca.crt
/var/lib/openbao-kms/identity.jwt
/run/openbao-kms/kms.sock
```

Recommended ownership:

```text
/usr/local/bin/bao-kms-provider    root:root                       0755
/etc/openbao-kms                   root:root                       0750
/etc/openbao-kms/config.yaml       root:openbao-kms                0640
/etc/openbao-kms/tls/ca.crt        root:root                       0644
/var/lib/openbao-kms               openbao-kms:openbao-kms         0750
/var/lib/openbao-kms/identity.jwt  root:openbao-kms                0640
/run/openbao-kms                   openbao-kms:openbao-kms-socket  2750
```

The provider runs as a non-root user (`openbao-kms`). The Kubernetes API server connects to the socket through the supplementary `openbao-kms-socket` group, which keeps API-server socket access separate from access to the provider JWT. For the full identity model and rationale, see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

For the configuration file shape and field reference, see [Configuration](/reference/configuration/).

## Run Doctor

Before starting the provider, run the bootstrap check:

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml
```

`doctor` validates:

- configuration file permissions and shape,
- JWT file readability and expiry,
- OpenBao TLS reachability,
- JWT login against the configured OpenBao role,
- Transit key metadata read and capability negation,
- probe encrypt and decrypt operations,
- key_id stability across probe operations,
- socket directory ownership and permissions.

Run `doctor` with the new artifact on every control-plane node before promoting the binary or image. For the full doctor flag set, see [Reference: CLI](/reference/cli/).

## Read Next

1. [Deployment: Choosing A Model](/deployment/choosing-a-model/) to decide between systemd and static pod.
2. [Kubernetes Encryption Config](/getting-started/kubernetes-encryption-config/) to wire the provider into the Kubernetes API server.
