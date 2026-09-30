---
title: Verify release artifacts
description: "What each release verification check proves, and where the install procedures run the checks for artifacts and the provider image."
eyebrow: Security · Supply chain
weight: 45
verifiedBy:
  - .github/workflows/release.yml
  - .github/workflows/reusable-build.yml
  - hack/tools/release_bundle/main.go
  - hack/install/download-release.sh
---

The provider runs on the Kubernetes API server boot path with access to the
Transit key that protects cluster data. Verify every artifact and image before
it reaches a control-plane node, and verify again for every upgrade.

The procedures run the checks where you use the artifact:

- [Download the release](/docs/get-started/download/#download-and-verify-the-artifact)
  verifies the package, tarball, or static-pod bundle you download.
- [Run as a static pod](/docs/get-started/static-pod/#step-2-verify-the-provider-image)
  verifies the provider image digest the static pod runs.

## Verification material

Each release publishes:

- `checksums.txt`, the SHA-256 checksum of every artifact,
- `checksums.txt.bundle`, a keyless cosign signature bundle over the checksum
  file,
- a GitHub build-provenance attestation for each artifact and for the image,
- a software bill of materials (SBOM) for each binary and image,
- a reproducibility report,
- `provenance-index.json`, an index of the provenance material.

## Verify build provenance

[Download the release](/docs/get-started/download/#download-and-verify-the-artifact)
verifies the checksum signature and the artifact checksum with `cosign`. The
build-provenance attestation adds that GitHub-hosted runners built the artifact
from the selected tag. Checking it needs an authenticated GitHub CLI (`gh`).
From the download directory:

```sh
gh attestation verify "./${ARTIFACT}" \
  --repo "${REPO}" \
  --signer-workflow "${REPO}/.github/workflows/release.yml" \
  --source-ref "refs/tags/${VERSION}" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com \
  --deny-self-hosted-runners
```

The command exits with status `0` and names the expected repository, workflow,
source tag, and artifact digest. Stop if it fails.

## Transfer to a disconnected environment

Perform download and signature/provenance verification on a connected staging
host. Transfer the selected artifact, `checksums.txt`, `checksums.txt.bundle`,
and `selected-checksum.txt` together over your approved transfer path. On the
destination, run:

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

## What each check proves

| Check | Proves | Identity it checks |
|---|---|---|
| `sha256sum --check` | The downloaded file matches the release checksum file. | None on its own. |
| `cosign verify-blob` on `checksums.txt` | The checksum file was signed by this repository's release workflow for the selected tag. | `.github/workflows/release.yml@refs/tags/<version>` |
| `gh attestation verify` on the artifact | The artifact was built by the release workflow from the selected tag on GitHub-hosted runners. | `release.yml`, source ref `refs/tags/<version>` |
| `cosign verify` on the image | The image digest was signed by the release workflow for the selected tag. | `release.yml@refs/tags/<version>` |
| `gh attestation verify` on the image | The image was built by the reusable build workflow from the selected tag on GitHub-hosted runners. | `reusable-build.yml`, source ref `refs/tags/<version>` |

The checksum check only means something after the signature check succeeds.
The signature and checksum checks are required; the attestation checks are
recommended where an authenticated GitHub CLI is available. Stop if any check
you run fails.

## Rules

- Pin every artifact and image to an exact release. Never install from a
  floating tag, branch, or `latest`.
- Run the static-pod image by the verified digest from `image-ref.txt`. Never
  replace it with a tag-only reference.
- Keep the previous verified artifact or image available on every
  control-plane node for rollback.

For the build and release controls that produce this material, see
[Contribute: CI and supply chain](/contribute/ci-supply-chain/).
