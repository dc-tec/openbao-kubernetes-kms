---
title: "Install"
description: "Fetch a verified provider binary or container image, place the runtime files, and validate the local environment with doctor."
weight: 30
---

# Install

Complete [OpenBao Setup](/getting-started/openbao-setup/) before installing the
provider. Then fetch a verified `bao-kms-provider` artifact, place the runtime
files, and validate the local environment before wiring the provider into
Kubernetes.

## Release Artifacts

Every public release publishes native packages, tarballs, a static-pod bundle,
a container image, and verification files such as checksums, signatures,
software bills of materials (SBOMs), and provenance attestations.

Verify the release artifacts before placing the provider on a control-plane
host, and validate the deployment in a staging environment before using it to
protect cluster data. See [Support Policy](/reference/support-policy/) for the
current release maturity.

## Choose The Artifact

Choose the artifact that matches the deployment model you will use.

| Form | Use | Details |
|---|---|---|
| Native package | systemd deployment | `.deb` or `.rpm` with systemd unit, sysusers, tmpfiles, examples, checksum, signature, and attestation. |
| Linux binary tarball | systemd deployment fallback | Deterministic tarball with binary, systemd metadata, examples, checksum, signature, and attestation. |
| Static-pod bundle | static-pod deployment | Deterministic tarball with static-pod manifest, provider config sample, encryption config, image reference, checksum, signature, and attestation. |
| Container image | static-pod runtime | Distroless non-root image, runs as `65532:65532`, published and verified by image digest. The runtime base image is pinned by digest in `.ci/versions.yaml`. |

Published release artifacts use the default JSON Web Token (JWT)-only auth
build. Certificate auth is an opt-in build variant:

```sh
make build-certauth-pkcs11
```

Certificate-auth artifacts that use the PKCS#11 Cryptographic Token Interface
are separate host builds. Build them with:

```sh
make release-artifact-certauth-pkcs11-host
```

PKCS#11 builds require cgo and a runtime PKCS#11 module on the host. PKCS#11 is
an opt-in preview path only when the selected release publishes the matching
artifact and marks that path as tested. `auth.cert.source: spiffe`, which
selects SPIFFE workload identity, is not a supported preview user
configuration.

Release artifacts distinguish these auth paths:

| Artifact family | Preview support |
|---|---|
| Default `bao-kms-provider` artifacts | JWT auth only. |
| `bao-kms-provider-certauth-pkcs11` host artifacts | PKCS#11 certificate auth only when the release marks that path as tested. |
| SPIFFE or combined cert-auth artifacts | Not a supported preview user configuration. |

The choice between systemd and static-pod is made on a separate page. See [Deployment: Choosing A Model](/deployment/choosing-a-model/) once the artifact is in place.

## Verify Release Artifacts

Verify the artifact before placing it on a control-plane host. The release
verification files include:

- a checksum file (`checksums.txt`),
- a keyless cosign signature bundle for the checksum file (`checksums.txt.bundle`),
- an SBOM per binary and per image,
- GitHub build-provenance attestations generated during the release workflow,
- a reproducibility report,
- a provenance index (`provenance-index.json`).

Verify in this order:

1. Fetch the artifact and the checksum file from the release page.
2. Compare the SHA-256 of the artifact against the entry in the checksum file.
3. Verify the signature over the checksum file against the release workflow identity.
4. Verify the artifact provenance attestation against the release workflow identity.
5. For static-pod deployments, verify the image signature and image provenance for the digest referenced by the static-pod bundle.

Do not replace the release image digest with a tag-only reference. The static
pod manifest must use the verified image digest from the selected release.

The examples below use `cosign` and the GitHub CLI.

Example:

```sh
VERSION=0.1.0
REPO=dc-tec/openbao-kubernetes-kms
WORKFLOW_IDENTITY="https://github.com/${REPO}/.github/workflows/release.yml@refs/tags/${VERSION}"

sha256sum --check --ignore-missing checksums.txt

cosign verify-blob \
  --new-bundle-format=true \
  --bundle checksums.txt.bundle \
  --certificate-identity "${WORKFLOW_IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

gh attestation verify ./bao-kms-provider_${VERSION}_linux_amd64 \
  --repo "${REPO}" \
  --signer-workflow "${REPO}/.github/workflows/release.yml" \
  --source-ref "refs/tags/${VERSION}" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com \
  --deny-self-hosted-runners
```

For the provider image, verify by digest:

```sh
IMAGE="ghcr.io/dc-tec/bao-kms-provider@sha256:<digest>"

cosign verify \
  --new-bundle-format=true \
  --certificate-identity "${WORKFLOW_IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "${IMAGE}"

gh attestation verify "oci://${IMAGE}" \
  --repo "${REPO}" \
  --signer-workflow "${REPO}/.github/workflows/release.yml" \
  --source-ref "refs/tags/${VERSION}" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com \
  --deny-self-hosted-runners
```

Successful checksum verification prints `<artifact>: OK` for the selected
artifact. Each signature and attestation command must exit with status `0` and
identify the expected repository, workflow, source tag, and artifact digest.

For the full supply-chain controls behind these artifacts, see
[Development: CI And Supply Chain](/development/ci-supply-chain/).

## Install A Native Package

Use the native package when deploying with systemd on a supported Linux distribution.

Debian or Ubuntu:

```sh
sudo dpkg -i bao-kms-provider_<version>_linux_amd64.deb
```

RHEL-family distributions:

```sh
sudo rpm -Uvh bao-kms-provider_<version>_linux_amd64.rpm
```

The package installs the binary, systemd unit, sysusers and tmpfiles inputs, and example configuration files. Review and replace the example configuration before starting the service.

## Install The systemd Tarball

Use the systemd tarball when native packaging is not available for your host image:

```sh
sudo tar -C / -xzf bao-kms-provider_<version>_systemd_linux_amd64.tar.gz
```

Then create the `openbao-kms` user, `openbao-kms` group, and
`openbao-kms-socket` group according to [Deployment: Linux Identity
Model](/deployment/linux-identity-model/). The tarball contains installable
files, but the host image remains responsible for distribution-specific user
and group creation.

## Install The Static-Pod Bundle

Use the static-pod bundle when the provider will run as a kubelet-managed static pod:

```sh
tar -xzf bao-kms-provider_<version>_static-pod.tar.gz
```

The bundle contains the static pod manifest, provider configuration sample, Kubernetes `EncryptionConfiguration` sample, and image reference. Before placing the manifest under `/etc/kubernetes/manifests/`, replace:

- the image digest with the verified digest from the selected release,
- the numeric `supplementalGroups` entry,
- the provider config values,
- the OpenBao CA path,
- the configured auth material paths.

Preload the referenced image on every control-plane node before relying on it for recovery-sensitive boot. See [Deployment: Static Pod Deployment](/deployment/static-pod/) for the host preparation and pod hardening details.

## Place Runtime Files

Recommended host layout:

```text
/usr/bin/bao-kms-provider
/etc/openbao-kms/config.yaml
/etc/openbao-kms/tls/ca.crt
/var/lib/openbao-kms/identity.jwt
/etc/openbao-kms/client/client-chain.pem
/etc/openbao-kms/pkcs11/pin
/run/openbao-kms/kms.sock
```

Recommended ownership:

```text
/usr/bin/bao-kms-provider          root:root                       0755
/etc/openbao-kms                   root:root                       0750
/etc/openbao-kms/config.yaml       root:openbao-kms                0640
/etc/openbao-kms/tls/ca.crt        root:root                       0644
/var/lib/openbao-kms               openbao-kms:openbao-kms         0750
/var/lib/openbao-kms/identity.jwt  root:openbao-kms                0640
/etc/openbao-kms/client/client-chain.pem root:openbao-kms           0640
/etc/openbao-kms/pkcs11/pin        root:openbao-kms                0640
/run/openbao-kms                   openbao-kms:openbao-kms-socket  2750
```

The provider runs as the non-root `openbao-kms` user. The Kubernetes API server
connects to the socket through the supplementary `openbao-kms-socket` group.
This group keeps API server socket access separate from access to provider auth
material. For the full identity model and rationale, see [Deployment: Linux
Identity Model](/deployment/linux-identity-model/).

For the configuration file shape and field reference, see [Configuration](/reference/configuration/).

## Validate Before Kubernetes Wiring

Inspect the resolved configuration:

```sh
bao-kms-provider config \
  --config /etc/openbao-kms/config.yaml
```

Verify the Transit key profile:

```sh
bao-kms-provider verify-key \
  --config /etc/openbao-kms/config.yaml
```

Run the bootstrap check:

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml
```

The configuration command must exit with status `0` and print the resolved
identity fingerprint. `verify-key` and `doctor` must exit with status `0` and
must not report any `[fail]` checks.

`doctor` validates:

- configuration file permissions and shape,
- auth material readability and local validity,
- OpenBao TLS reachability,
- login against the configured OpenBao auth method and role,
- Transit key metadata read and capability negation,
- probe encrypt and decrypt operations,
- key_id stability across probe operations,
- socket directory ownership and permissions.

After the Kubernetes `EncryptionConfiguration` exists, run `doctor` with that file too:

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/encryption-config.yaml
```

Run these checks with the new artifact on every control-plane node before promoting the binary or image. For the full command reference, see [Reference: CLI](/reference/cli/).

## Read Next

1. [Deployment: Choosing A Model](/deployment/choosing-a-model/) to decide between systemd and static pod.
2. [Kubernetes Encryption Config](/getting-started/kubernetes-encryption-config/) to wire the provider into the Kubernetes API server.
