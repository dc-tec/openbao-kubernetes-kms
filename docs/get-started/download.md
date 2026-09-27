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
| Static pod | `bao-kms-provider_<version>_static-pod.tar.gz` | Static pod manifest, provider configuration sample, `EncryptionConfiguration` sample, and the provider image digest in `image-ref.txt`. |

The static-pod bundle references the distroless provider image, which runs as
`65532:65532` and is always pulled by digest.

Published artifacts use JSON Web Token (JWT) auth. PKCS#11 certificate auth
needs a separate build; see
[Configure: OpenBao auth and policy](/docs/configure/openbao-auth/).

## Download and verify the artifact

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
