---
title: Verify release artifacts
description: "What each release verification check proves, and where the install procedures run the checks for artifacts and the provider image."
eyebrow: Security · Supply chain
weight: 45
verifiedBy:
  - .github/workflows/release.yml
  - .github/workflows/reusable-build.yml
  - hack/tools/release_bundle/main.go
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

## What each check proves

| Check | Proves | Identity it checks |
|---|---|---|
| `sha256sum --check` | The downloaded file matches the release checksum file. | None on its own. |
| `cosign verify-blob` on `checksums.txt` | The checksum file was signed by this repository's release workflow for the selected tag. | `.github/workflows/release.yml@refs/tags/<version>` |
| `gh attestation verify` on the artifact | The artifact was built by the release workflow from the selected tag on GitHub-hosted runners. | `release.yml`, source ref `refs/tags/<version>` |
| `cosign verify` on the image | The image digest was signed by the release workflow for the selected tag. | `release.yml@refs/tags/<version>` |
| `gh attestation verify` on the image | The image was built by the reusable build workflow from the selected tag on GitHub-hosted runners. | `reusable-build.yml`, source ref `refs/tags/<version>` |

The checksum check only means something after the signature check succeeds.
Run all checks for the artifact you install, and stop if any check fails.

## Rules

- Pin every artifact and image to an exact release. Never install from a
  floating tag, branch, or `latest`.
- Run the static-pod image by the verified digest from `image-ref.txt`. Never
  replace it with a tag-only reference.
- Keep the previous verified artifact or image available on every
  control-plane node for rollback.

For the build and release controls that produce this material, see
[Contribute: CI and supply chain](/contribute/ci-supply-chain/).
