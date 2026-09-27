---
title: Download the release
description: "Choose the release artifact for your deployment model, then download it and verify its checksum, signature, and build provenance."
eyebrow: Get started · Step 4
weight: 40
verifiedBy:
  - .github/workflows/release.yml
  - hack/tools/release_bundle/main.go
  - deploy/package/bundles
---

Choose one artifact version for the whole installation. Verify it before it
reaches a control-plane node.

These Next docs describe the upcoming `init` workflow. The published
`0.1.0-preview.2` artifacts lack `init` and native OAuth. For those artifacts,
use the released documentation from the version selector. To evaluate Next,
use the binary, image, and installation kit built from the same selected
candidate commit; see [Contributing](/contribute/contributing/).

The download commands below apply after selecting a published release that
contains the features you intend to use.

## Choose the artifact

| Deployment model | Artifact | Contents |
|---|---|---|
| systemd on Debian or Ubuntu | `bao-kms-provider_<version>_linux_<arch>.deb` | Binary, systemd unit, sysusers and tmpfiles inputs, example configuration. |
| systemd on RHEL-family hosts | `bao-kms-provider_<version>_linux_<arch>.rpm` | Same as the Debian package. |
| systemd on other hosts | `bao-kms-provider_<version>_systemd_linux_<arch>.tar.gz` | Same files as the packages, installed with the commands in [Run with systemd](/docs/get-started/systemd/). |
| Static pod | `bao-kms-provider_<version>_static-pod_linux_<arch>.tar.gz` | Matching host binary, minimal values, manifest inputs, host preparation instructions, and image digest in `image-ref.txt`. |

The static-pod bundle references the distroless provider image, which runs as
`65532:65532` and is always pulled by digest.

Published artifacts use JSON Web Token (JWT) auth. PKCS#11 certificate auth
needs a separate build; see
[Configure: OpenBao auth and policy](/docs/configure/openbao-auth/).

## Download and verify the artifact

From a trusted checkout of the selected release, the helper selects the file
name and runs the checks below:

```sh
bash hack/install/download-release.sh "$VERSION" static-pod "$ARCH" "kms-${VERSION}-${ARCH}"
```

Set `VERSION` and `ARCH` first. Choose `systemd`, `static-pod`, `deb`, or `rpm`.
The destination must not exist. The helper requires a complete version tag,
checks the signature before trusting checksums, requires the selected file's
checksum, and verifies its build attestation. It downloads without installing.
It is also published as `download-release.sh`; verify that asset using the
manual procedure before running it. The architecture-specific static-pod
selector applies to the upcoming release, not the preview.2 bundle layout.

You need `cosign`, an authenticated GitHub CLI (`gh`), and GNU `sha256sum`.
Set `VERSION` to the release you install, `ARCH` to `amd64` or `arm64`, and
`ARTIFACT` to the file name from the table. Work in a new, empty directory so
the checksum check covers only the selected artifact:

```sh
VERSION=REPLACE_WITH_SELECTED_RELEASE
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

The checksum check prints `<artifact>: OK`. The signature and attestation
commands exit with status `0` and name the expected repository, workflow,
source tag, and artifact digest. Stop if any check fails. For what each check
proves, see [Security: Verify release artifacts](/docs/security/verify-release-artifacts/).

Keep this shell open: the next step uses `VERSION`, `ARCH`, and the
downloaded file.

The current release line is a preview. Use it for labs, staging, and
evaluation; see [Reference: Release and support lifecycle](/docs/reference/release-lifecycle/).

## Transfer to a disconnected environment

Perform download and signature/provenance verification on a connected staging
host. Transfer the selected artifact, `checksums.txt`, `checksums.txt.bundle`,
and `selected-checksum.txt` together over your approved transfer path. If you
used the manual procedure, create `selected-checksum.txt` with only the exact
artifact entry from the verified checksum file. On the destination, run:

```sh
sha256sum --check selected-checksum.txt
```

Every listed file must report `OK`. This checks transfer integrity against the
checksum trusted on the staging host; it does not repeat signature verification.
Keep the verified originals and the verification output in your release record.

For static pods, verify the OCI image as described in
[Run as a static pod](/docs/get-started/static-pod/#step-2-verify-the-provider-image).
On a connected Linux staging host with containerd, export the selected platform:

```sh
IMAGE=$(cat image-ref.txt)
sudo ctr -n k8s.io images pull --platform "linux/${ARCH}" "$IMAGE"
sudo ctr -n k8s.io images export --platform "linux/${ARCH}" provider-image.tar "$IMAGE"
sha256sum provider-image.tar > provider-image.tar.sha256
```

Transfer the image archive and its checksum with the kit. On each matching
control-plane host, verify the checksum and preload the image:

```sh
sha256sum --check provider-image.tar.sha256
sudo ctr -n k8s.io images import --platform "linux/${ARCH}" provider-image.tar
sudo crictl inspecti "$IMAGE" >/dev/null
```

Keep the digest reference in the generated manifest and `IfNotPresent` pull
policy. If using a registry mirror, copy all platforms while preserving image
digests, verify the destination digest against `image-ref.txt`, then pass that
mirror repository with the same digest to `init --image`. Stop if the digest
changes; a repackaged image is not the verified release image.

Transfer the selected release's instructions, OS dependencies, TLS trust
bundles, reviewed generated files, and independently provisioned credentials
through their respective trusted channels. The kit does not supply kubelet,
containerd, systemd, OpenBao, or the issuer. Check DNS, time synchronization,
OpenBao, and issuer reachability without Internet access or the protected API.
