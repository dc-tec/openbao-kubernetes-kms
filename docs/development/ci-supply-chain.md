---
title: "CI And Supply Chain"
description: "Version pinning, CI lanes, release automation, artifact signing, provenance, reproducibility, and release evidence for bao-kms-provider."
weight: 50
---

# CI And Supply Chain

This page defines how the project builds confidence in source changes and
public release artifacts. The same policy applies to code, deployment samples,
container images, packages, and documentation.

The release model is:

- pin versions in one manifest,
- run local checks through `make`,
- route heavier tests by changed area,
- build release artifacts from a tag,
- sign and attest the published subjects,
- verify byte reproducibility before publication,
- publish or retain evidence for every public release.

The repository commits the Go `vendor/` tree. CI and release jobs run with
`GOFLAGS=-mod=vendor` except where a target intentionally refreshes or verifies
module metadata.

## Version Policy

`.ci/versions.yaml` is the source of truth for toolchain, action, dependency,
container, OpenBao, Kubernetes, and artifact-version inputs. CI and release
workflows read from that file or are reviewed against it. Support claims require
exact pins.

The policy is:

- no floating `latest` inputs in CI, release workflows, or support claims,
- GitHub Actions pinned by commit SHA,
- Go toolchain pinned,
- Go dependencies vendored,
- OpenBao validation image pinned by digest,
- Kind node image pinned by digest,
- release container builder and runtime base images pinned by digest,
- release artifact names and checksum filenames defined by the version policy.

Initial validation uses OpenBao `2.5.3`, Kubernetes KMS v2, Linux
control-plane nodes, and exact-pinned Kind lanes for the Kubernetes `1.34` and
`1.35` release lines recorded in `.ci/versions.yaml`. Kubernetes `1.36` is the
intended next validation line once a digest-pinned Kind node image is available.
Future Kubernetes or OpenBao versions become support claims only after
exact-pinned release evidence exists. See
[Reference: Compatibility](/reference/compatibility/).

## Local Parity

Use the Makefile entry points locally. `make ci-core` is the standard fast gate:

```sh
make ci-core
```

It covers formatting, vetting, static analysis, vulnerability checks, Semgrep,
ast-grep rules, unit tests, race smoke, fuzz smoke, generated artifact checks,
vendored dependency verification, license checks, KMS v2 fake conformance, key
ID and AAD golden tests, configuration validation, redaction tests, and E2E
suite manifest validation.

Run focused E2E lanes when a change touches runtime, OpenBao, Kubernetes, or
deployment behavior. The canonical command list is [E2E Framework](/development/e2e-framework/).

## CI Lanes

### Pull Requests

Every pull request runs the fast quality and safety gates:

- formatting and Go static analysis,
- strict typed-Go architecture rules,
- Semgrep security and API-misuse rules,
- KMS v2 fake conformance,
- key ID and AAD golden tests,
- configuration validation,
- redaction tests,
- vendored dependency verification,
- license checks,
- vulnerability and static security scans.

Change-routed expansions add deeper checks:

| Changed area | Additional validation |
|---|---|
| `internal/kmsv2`, `internal/keyregistry`, `internal/aad` | conformance, fuzz smoke, golden fixtures |
| `internal/openbao`, `internal/auth` | hermetic OpenBao client integration and OpenBao CI E2E |
| `internal/socket`, deployment samples | systemd and static-pod staging checks |
| status or rotation code | rotation and failure-injection lanes |
| packaging or Dockerfile | image scan, SBOM smoke, reproducibility smoke |
| docs only | docs check and Hugo build |

### Main And Nightly

Main and scheduled lanes add the slower integration coverage:

- OpenBao `2.5.3` CI E2E,
- pinned Kubernetes Kind E2E,
- Kind multi-control-plane convergence,
- static-pod upgrade and rollback,
- Kind disaster-recovery runbook,
- OpenBao failure injection,
- OpenBao HA failover,
- Transit rotation,
- provider upgrade and rollback,
- provider and OpenBao load soak,
- sustained direct decrypt soak,
- image scan and SBOM generation.

The soak lanes are release evidence for the pinned CI environment only. They do
not establish a production SLO or broad capacity claim.

Local kubeadm VM validation stays outside public CI because it restarts VMs,
restarts API servers, and intentionally stops OpenBao in the validation
environment.

## Release Automation

release-please owns release PRs, version proposals, and `CHANGELOG.md`.
Publishing is a separate tag workflow.

release-please:

- opens or updates the release PR from Conventional Commits,
- updates `.release-please-manifest.json`,
- updates `CHANGELOG.md`,
- supports manual `Release-As` overrides,
- does not create tags,
- does not publish GitHub Releases,
- does not build, sign, attest, or upload artifacts.

The release-tag workflow:

- validates the merged release-please PR and release manifest,
- creates a signed annotated SemVer tag at the release PR merge commit,
- creates or refreshes a draft GitHub Release from the release-please notes,
- does not build, sign, attest, or upload release assets.

The tag release workflow:

- requires the draft GitHub Release created by the release-tag workflow,
- builds the image and release assets,
- independently rebuilds the image for reproducibility comparison,
- runs source, image, and the manifest-defined OpenBao and Kind preview release
  gates from `test/e2e/suites.yaml`,
- generates SBOMs,
- generates deterministic checksums,
- signs the image by digest,
- signs `checksums.txt` with a keyless cosign bundle,
- creates GitHub build-provenance attestations,
- verifies image and file attestations against the release workflow identity,
- verifies byte reproducibility,
- generates `provenance-index.json`,
- uploads byte-verified assets to the draft release,
- publishes the GitHub Release and GHCR image tag through the
  maintainer-controlled `release-publish` GitHub Environment.

Release credentials, signing keys, and tag-ruleset bypass are maintainer
configuration, not user-facing deployment inputs.

The tag release workflow uses GitHub `GITHUB_TOKEN`, OIDC, and attestations
permissions for asset publication, signing, and provenance.

Private repository release dry runs on user-owned repositories cannot persist
GitHub artifact attestations because GitHub does not expose that feature there.
In that mode the workflow skips GitHub attestation persistence and verification,
records `attestations.available: false` in `provenance-index.json`, and still
validates the build, E2E, reproducibility, signatures, checksums, SBOMs, and
published assets. Public release tags must run with attestations enabled.

## Supply-Chain Gates

Required before publishing any release artifact:

- pinned GitHub Actions by commit SHA,
- vendored Go dependencies,
- dependency review,
- license allowlist for shipped dependencies,
- static security scan,
- `govulncheck`,
- filesystem and image vulnerability scans,
- SBOMs for published binaries and images,
- checksums for release assets,
- signed release checksums,
- signed image digest,
- provenance attestations for release assets and image subjects,
- verification of attestations against the release workflow identity,
- byte reproducibility check for release images and SBOMs,
- release provenance index.

## Build Once, Promote By Digest

Release workflows build immutable subjects once, capture digests, verify trust
evidence, and publish by digest. Publication does not rebuild a different
subject.

```text
release-please PR merge
  -> signed tag and draft GitHub Release
  -> build image and release assets
  -> rebuild image independently
  -> capture digests and checksums
  -> generate SBOMs
  -> verify byte reproducibility
  -> sign and attest
  -> verify signatures and attestations
  -> upload release assets and provenance index to the draft release
  -> publish release
```

## Release Channels

| Channel | Use | Support expectation |
|---|---|---|
| PR | validation only | no public artifacts |
| main | integration signal for merged code | no production support |
| nightly | scheduled drift detection | not production |
| release candidate | pre-release soak for an intended tag | staging or evaluation |
| preview | tagged release for controlled validation | not production |
| stable | production-ready release line | only after production-readiness gates pass |

For channel rules see [Reference: Release Policy](/reference/release-policy/).

## Release Evidence

Every public release publishes or retains:

- source commit,
- release tag,
- workflow run URL,
- OpenBao and Kubernetes validation matrix,
- image digest,
- binary, package, and bundle checksums,
- SBOMs,
- vulnerability scan summary,
- image signature verification output,
- checksum signature verification output,
- GitHub provenance attestations,
- release artifact attestation verification output,
- byte-reproducibility report,
- `provenance-index.json`,
- release notes.

Install-time verification is documented in [Getting Started: Install](/getting-started/install/#verify-release-evidence).
