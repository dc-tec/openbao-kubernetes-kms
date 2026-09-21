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
| systemd tarball | systemd deployment fallback | Deterministic tarball with binary, systemd metadata, examples, checksum, signature, and attestation. |
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

The following Linux example uses `cosign`, an authenticated GitHub CLI, and
GNU `sha256sum`. It selects the published `0.1.0-preview.2` evaluation release.
Set `ARCH` to `amd64` or `arm64` for the target host. Set `ARTIFACT` to the
systemd tarball, native package, or static-pod bundle you intend to install:

- systemd tarball: `bao-kms-provider_${VERSION}_systemd_linux_${ARCH}.tar.gz`
- Debian package: `bao-kms-provider_${VERSION}_linux_${ARCH}.deb`
- RPM package: `bao-kms-provider_${VERSION}_linux_${ARCH}.rpm`
- static-pod bundle: `bao-kms-provider_${VERSION}_static-pod.tar.gz`

Use a new download directory so checksum verification covers only the selected
artifact.

```sh
VERSION=0.1.0-preview.2
ARCH=amd64
ARTIFACT="bao-kms-provider_${VERSION}_systemd_linux_${ARCH}.tar.gz"
REPO=dc-tec/openbao-kubernetes-kms
WORKFLOW_IDENTITY="https://github.com/${REPO}/.github/workflows/release.yml@refs/tags/${VERSION}"

mkdir "kms-${VERSION}-${ARCH}"
cd "kms-${VERSION}-${ARCH}"
gh release download "${VERSION}" --repo "${REPO}" \
  --pattern "${ARTIFACT}" \
  --pattern checksums.txt \
  --pattern checksums.txt.bundle

sha256sum --check --ignore-missing checksums.txt

cosign verify-blob \
  --new-bundle-format=true \
  --bundle checksums.txt.bundle \
  --certificate-identity "${WORKFLOW_IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

gh attestation verify "./${ARTIFACT}" \
  --repo "${REPO}" \
  --signer-workflow "${REPO}/.github/workflows/release.yml" \
  --source-ref "refs/tags/${VERSION}" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com \
  --deny-self-hosted-runners
```

For the provider image, extract the verified static-pod bundle and read
`image-ref.txt`. Replace `<digest>` below with the SHA-256 digest from that
reference. The release workflow signs the image; the
reusable build workflow creates its build-provenance attestation:

```sh
IMAGE="ghcr.io/dc-tec/bao-kms-provider@sha256:<digest>"

cosign verify \
  --new-bundle-format=true \
  --certificate-identity "${WORKFLOW_IDENTITY}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "${IMAGE}"

gh attestation verify "oci://${IMAGE}" \
  --repo "${REPO}" \
  --signer-workflow "${REPO}/.github/workflows/reusable-build.yml" \
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
sudo dpkg -i "bao-kms-provider_${VERSION}_linux_${ARCH}.deb"
```

RHEL-family distributions:

```sh
sudo rpm -Uvh "bao-kms-provider_${VERSION}_linux_${ARCH}.rpm"
```

The package installs the binary, systemd unit, sysusers and tmpfiles inputs, and example configuration files. Review and replace the example configuration before starting the service.

## Install The systemd Tarball

Use the systemd tarball when native packaging is not available for your host
image. These commands require GNU `install`, `systemd-sysusers`, and
`systemd-tmpfiles`. Hosts without these tools must provision the equivalent
layout through configuration management. See [Linux Identity
Model](/deployment/linux-identity-model/).

The archive contains a versioned directory. Extract it in the download
directory, then enter it using the `VERSION` and `ARCH` selected above:

<!-- systemd-bundle-extract -->
```sh
tar -xzf "bao-kms-provider_${VERSION}_systemd_linux_${ARCH}.tar.gz"
cd "bao-kms-provider_${VERSION}_systemd_linux_${ARCH}"
```

Run the following block in a root shell from the extracted directory. It
installs the files and creates the service identities and directories. It
leaves live configuration and authentication material unchanged.

<!-- systemd-bundle-install -->
```sh
set -eu
install -D -o root -g root -m 0755 bin/bao-kms-provider /usr/bin/bao-kms-provider
install -D -o root -g root -m 0644 systemd/bao-kms-provider.service /usr/lib/systemd/system/bao-kms-provider.service
install -D -o root -g root -m 0644 sysusers.d/openbao-kms.conf /usr/lib/sysusers.d/openbao-kms.conf
install -D -o root -g root -m 0644 tmpfiles.d/openbao-kms.conf /usr/lib/tmpfiles.d/openbao-kms.conf
install -D -o root -g root -m 0644 config/provider-systemd.yaml /usr/share/doc/bao-kms-provider/examples/provider-systemd.yaml
install -D -o root -g root -m 0644 kubernetes/encryption-config.yaml /usr/share/doc/bao-kms-provider/examples/encryption-config.yaml
install -D -o root -g root -m 0644 README.md /usr/share/doc/bao-kms-provider/README.md
install -D -o root -g root -m 0644 LICENSE /usr/share/doc/bao-kms-provider/LICENSE
systemd-sysusers /usr/lib/sysusers.d/openbao-kms.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/openbao-kms.conf
```

Reload systemd to register the installed unit:

```sh
systemctl daemon-reload
```

The package and tarball procedures leave the service disabled and stopped on a
new host. Complete the runtime file setup and validation before starting it.

## Correct Directory Access In Existing Previews

The native packages and systemd tarballs for `0.1.0-preview.1` and
`0.1.0-preview.2` create `/etc/openbao-kms` with group `root` and mode `0750`.
The service user cannot traverse that directory. On these releases, run the
following block as root after installation. It creates a persistent tmpfiles
override so the correction survives reboot. If an override already exists,
update its `/etc/openbao-kms` entry manually instead of replacing it.

<!-- systemd-preview-permissions -->
```sh
set -eu
test ! -e /etc/tmpfiles.d/openbao-kms.conf
install -d -o root -g root -m 0755 /etc/tmpfiles.d
sed 's@^d /etc/openbao-kms 0750 root root -$@d /etc/openbao-kms 0750 root openbao-kms -@' \
  /usr/lib/tmpfiles.d/openbao-kms.conf > /etc/tmpfiles.d/openbao-kms.conf
chmod 0644 /etc/tmpfiles.d/openbao-kms.conf
systemd-tmpfiles --create /etc/tmpfiles.d/openbao-kms.conf
```

Keep this override until the installed package's
`/usr/lib/tmpfiles.d/openbao-kms.conf` sets the directory group to
`openbao-kms`. Then remove the override if it contains no other local changes.

## Install The Static-Pod Bundle

Use the static-pod bundle when the provider will run as a kubelet-managed static pod:

```sh
tar -xzf "bao-kms-provider_${VERSION}_static-pod.tar.gz"
cd "bao-kms-provider_${VERSION}_static-pod"
```

The bundle contains the static pod manifest, provider configuration sample, Kubernetes `EncryptionConfiguration` sample, and image reference. Before placing the manifest under `/etc/kubernetes/manifests/`, replace:

- the image digest with the verified digest from the selected release,
- the numeric `supplementalGroups` entry,
- the provider config values,
- the OpenBao CA path,
- the configured auth material paths.

Preload the referenced image on every control-plane node before relying on it for recovery-sensitive boot. See [Deployment: Static Pod Deployment](/deployment/static-pod/) for the host preparation and pod hardening details.

## Place Runtime Files

For systemd, prepare these files after installing the package or tarball.
Static-pod deployments require the numeric ownership described in
[Static Pod Deployment](/deployment/static-pod/).

On a new systemd host, copy the installed example to a working file:

```sh
cp /usr/share/doc/bao-kms-provider/examples/provider-systemd.yaml provider.yaml
```

Replace the OpenBao address, TLS server name, JWT claims and paths, Transit key, and
`keyIdScope` values with the values from [OpenBao
Setup](/getting-started/openbao-setup/). Obtain the OpenBao CA bundle and JWT
from your identity provisioning process. Do not use the sample values or a
JWT that cannot be renewed independently of the protected API server.

From the directory containing the prepared files, run as root:

<!-- systemd-runtime-files -->
```sh
set -eu
test ! -e /etc/openbao-kms/config.yaml
test ! -e /etc/openbao-kms/tls/ca.crt
test ! -e /var/lib/openbao-kms/identity.jwt
install -o root -g openbao-kms -m 0640 provider.yaml /etc/openbao-kms/config.yaml
install -o root -g root -m 0644 ca.crt /etc/openbao-kms/tls/ca.crt
install -o root -g openbao-kms -m 0640 identity.jwt /var/lib/openbao-kms/identity.jwt
```

These commands are for initial setup. For an existing deployment, follow
[Upgrade](/operations/upgrade/) and preserve its identity and state.
The default release uses JWT auth; certificate and PKCS#11 files are needed
only for the corresponding opt-in build.

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
/etc/openbao-kms                  root:openbao-kms                0750
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

For systemd, run the checks as `openbao-kms` so unreadable files cause the same
failure as they would in the service. For static pods, run diagnostics with the
container identity and mounts described in [Static Pod
Deployment](/deployment/static-pod/).

Inspect the resolved configuration:

```sh
sudo -u openbao-kms bao-kms-provider config \
  --config /etc/openbao-kms/config.yaml
```

Verify the Transit key profile:

```sh
sudo -u openbao-kms bao-kms-provider verify-key \
  --config /etc/openbao-kms/config.yaml
```

Run the bootstrap check:

```sh
sudo -u openbao-kms bao-kms-provider doctor \
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

After the Kubernetes `EncryptionConfiguration` exists, run `doctor` with that
file too. If the service user cannot read the API server's configuration, run
this additional check as root after the service-user check passes:

```sh
sudo bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/encryption-config.yaml
```

Run these checks with the new artifact on every control-plane node before promoting the binary or image. For the full command reference, see [Reference: CLI](/reference/cli/).

## Read Next

1. [Deployment: Choosing A Model](/deployment/choosing-a-model/) to decide between systemd and static pod.
2. [Kubernetes Encryption Config](/getting-started/kubernetes-encryption-config/) to wire the provider into the Kubernetes API server.
