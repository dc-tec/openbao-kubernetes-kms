---
title: Download the release
description: "Choose the release artifact for your deployment model, then download it and verify its checksum, signature, and build provenance."
eyebrow: Get started · Step 2
weight: 20
verifiedBy:
  - .github/workflows/release.yml
  - hack/tools/release_bundle/main.go
  - deploy/package/bundles
---

Choose one release version for the whole installation. Verify the artifact
before it reaches a control-plane node.

## Choose the artifact

| Deployment model | Artifact | Contents |
|---|---|---|
| systemd on Debian or Ubuntu | `bao-kms-provider_<version>_linux_<arch>.deb` | Binary, systemd unit, sysusers and tmpfiles inputs, example configuration. |
| systemd on RHEL-family hosts | `bao-kms-provider_<version>_linux_<arch>.rpm` | Same as the Debian package. |
| systemd on other hosts | `bao-kms-provider_<version>_systemd_linux_<arch>.tar.gz` | Same files as the packages, installed with the commands in [Run with systemd](/docs/get-started/systemd/). |
| Static pod | `bao-kms-provider_<version>_static-pod_linux_<arch>.tar.gz` | Matching host binary, minimal values, manifest inputs, host preparation instructions, and image digest in `image-ref.txt`. |

Workstations need the CLI for `init` and `policy`. Use the plain
`bao-kms-provider_<version>_linux_<arch>` binary on Linux, or
`bao-kms-provider_<version>_darwin_<arch>` on macOS (`amd64` or `arm64`). The
macOS binary runs workstation commands only; `serve` refuses to start outside
Linux. Download it with `curl` as below, then make it executable with
`chmod +x`. A browser download is quarantined, and macOS blocks the unsigned
binary. On Windows, use the Linux binary inside WSL2.

The static-pod bundle references the distroless provider image, which runs as
`65532:65532` and is always pulled by digest. Published artifacts use JSON Web
Token (JWT) auth; PKCS#11 certificate auth needs a separate build, see
[Configure: OpenBao auth and policy](/docs/configure/openbao-auth/#certificate-auth).

## Download and verify the artifact

You need `curl` and `cosign`. Set `VERSION` to the release you install, `ARCH`
to `amd64` or `arm64`, and `ARTIFACT` to the file name from the table. Work in
a new, empty directory:

```sh
VERSION=REPLACE_WITH_SELECTED_RELEASE
ARCH=amd64
ARTIFACT="bao-kms-provider_${VERSION}_static-pod_linux_${ARCH}.tar.gz"
REPO=dc-tec/openbao-kubernetes-kms
BASE="https://github.com/${REPO}/releases/download/${VERSION}"

mkdir "kms-${VERSION}-${ARCH}"
cd "kms-${VERSION}-${ARCH}"
curl -fsSL --remote-name-all \
  "${BASE}/${ARTIFACT}" "${BASE}/checksums.txt" "${BASE}/checksums.txt.bundle"
```

Check the signature on the checksum file first. It proves that this
repository's release workflow signed the file for the selected tag:

```sh
cosign verify-blob \
  --new-bundle-format=true \
  --bundle checksums.txt.bundle \
  --certificate-identity "https://github.com/${REPO}/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

Then check the artifact against its signed checksum. On macOS, use
`shasum -a 256 --check` instead of `sha256sum --check`:

```sh
awk -v file="${ARTIFACT}" '$2 == file' checksums.txt > selected-checksum.txt
sha256sum --check selected-checksum.txt
```

`cosign` prints `Verified OK` and the checksum check prints `<artifact>: OK`.
Stop if either fails, or if `selected-checksum.txt` is empty.

To also verify the build provenance of the artifact, run `gh attestation verify`
as described in
[Security: Verify release artifacts](/docs/security/verify-release-artifacts/#verify-build-provenance).
It needs an authenticated GitHub CLI. For a disconnected environment, see
[Transfer to a disconnected environment](/docs/security/verify-release-artifacts/#transfer-to-a-disconnected-environment).

Keep this shell open: the next steps use `VERSION`, `ARCH`, and the
downloaded file.

Continue with [Generate installation files](/docs/get-started/plan-values/).
