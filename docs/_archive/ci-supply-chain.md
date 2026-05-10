# CI And Supply Chain

This document defines the intended CI, release, and supply-chain posture for the KMS provider.

The design follows the OpenBao Operator CI pattern where it applies:

- local parity through `make` targets,
- central version policy,
- no floating `latest` versions in CI or release gates,
- change-routed test expansion,
- vendored and deterministic Go builds,
- pinned GitHub Actions and tool versions,
- pinned Ginkgo/Gomega e2e framework versions,
- validated `test/e2e/suites.yaml` e2e lane manifest,
- license and vulnerability gates,
- SBOM generation,
- provenance and signature evidence,
- release reproducibility checks before promotion.

## Version Pinning Policy

CI must not use floating `latest` inputs for support claims.

Version policy lives in `.ci/versions.yaml`. CI and release workflows must read support-matrix inputs from that file instead of duplicating version strings in workflow definitions.

```yaml
version: 1
project:
  name: openbao-kubernetes-kms
  module: github.com/dc-tec/openbao-kubernetes-kms
  binary: bao-kms-provider
toolchain:
  go: "1.26.3"
  commandFramework:
    cliConfig: viper
  qualityTools:
    astGrep: "0.42.1"
    gofumpt: "0.9.2"
    staticcheck: "0.7.0"
    govulncheck: "1.2.0"
    semgrep: "1.157.0"
    golangciLint: "2.11.4"
    pinStatus: pinned
  githubActions:
    checkout: de0fac2e4500dabe0009e67214ff5f5447ce83dd
    createGithubAppToken: 1b10c78c7865c340bc4f6099eb2f838309f1e8c3
    releasePlease: 45996ed1f6d02564a971a2fa1b5860e934307cf7
    setupGo: 4b73464bb391d4059bd26b0524d20df3927bd417
    setupNode: 48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e
  goDependencies:
    ginkgo: "2.28.3"
    gomega: "1.40.0"
  testFrameworks:
    e2e: ginkgo
    assertions: gomega
    suiteManifest: test/e2e/suites.yaml
    reportFormats:
      - ginkgo-json
      - junit
validation:
  openbao:
    primary: "2.5.3"
    image: "ghcr.io/openbao/openbao:2.5.3"
    imageDigest: "sha256:fdc6da21ca6963560c32336fd7feb9cf2d5e52668f1a1647205a4b41171f0806"
    digestStatus: pinned
  kubernetes:
    minimumLine: "1.34"
    primaryLine: "1.34"
    upstreamPatch: "1.34.7"
    exactVersion: "1.34.3"
    kindNodeImage: "kindest/node:v1.34.3@sha256:08497ee19eace7b4b5348db5c6a1591d7752b164530a36f855cb0f2bdcbadd48"
    kindNodeImageDigest: "sha256:08497ee19eace7b4b5348db5c6a1591d7752b164530a36f855cb0f2bdcbadd48"
    pinStatus: pinned-kind-node
    futureCandidates:
      - "1.35"
      - "1.36"
```

Kubernetes upstream currently lists `1.34.7` as the latest `1.34` patch. The Kind e2e lane pins `kindest/node:v1.34.3` by digest because that is the newest official Kind node image available for the `1.34` line. Do not treat `1.34.7` as validated until an exact-pinned runnable Kubernetes lane exists for that patch.

## Initial Validation Matrix

Initial v0.1 validation target:

| Component | Version posture |
|---|---|
| OpenBao | `2.5.3` only. |
| Kubernetes | `1.34.3` in Kind for the initial e2e lane; `1.34.7` tracked as latest upstream patch, not validated. |
| KMS API | Kubernetes KMS v2 only. |
| OS | Linux control-plane nodes only. |

Kubernetes `1.35` and newer are future candidates, not implicit support claims. Add them only after exact patch versions, Kind node images, kubeadm VM coverage, and release gates are in place.

## Local Parity

Expected local entry points:

```sh
make bootstrap
make doctor
make ci-core
```

`make ci-core` should cover:

- format,
- gofumpt,
- lint,
- `go vet`,
- staticcheck,
- govulncheck,
- golangci-lint using `.golangci.yml`,
- ast-grep structural rule tests and scan using `.ast-grep/sgconfig.yml`,
- Semgrep security/API-misuse rule tests and scan using `.semgrep/rules`,
- unit tests,
- race smoke tests,
- ast-grep forbidden dynamic Go type rules,
- generated artifact checks,
- E2E suite manifest validation,
- vendored dependency check,
- license check,
- static security scan,
- fuzz smoke,
- KMS v2 fake conformance,
- key ID/AAD golden tests,
- config validation tests,
- redaction tests.

## Release PR Automation

release-please owns release PR creation, version proposals, and `CHANGELOG.md` updates. It is intentionally separated from publishing:

- release-please may update `.release-please-manifest.json` and `CHANGELOG.md`,
- release-please must run from a pinned action SHA,
- release-please uses a GitHub App token when configured,
- release-please skips GitHub Release creation,
- artifact builds, checksums, SBOMs, signatures, attestations, and provenance remain release workflow responsibilities.

Required repository secrets for active release PR automation:

- `OPENBAO_KMS_RELEASE_PR_APP_ID`
- `OPENBAO_KMS_RELEASE_PR_PRIVATE_KEY`

## CI Lanes

### Pull Requests

Required for every PR:

- core quality gate,
- strict typed-Go quality gate,
- ast-grep architecture/runtime-safety scan,
- Semgrep security/API-misuse scan,
- KMS v2 fake conformance,
- key ID/AAD golden tests,
- redaction tests,
- config validation tests,
- vendored dependency verification,
- license check,
- vulnerability/static security scan.

Change-routed expansions:

| Changed area | Additional validation |
|---|---|
| `internal/kmsv2`, `internal/keyregistry`, `internal/aad` | conformance, fuzz smoke, golden fixtures. |
| `internal/openbao`, `internal/auth` | Hermetic OpenBao client integration lane. |
| `internal/socket`, deployment samples | systemd/static-pod smoke checks. |
| rotation/status code | rotation and failure-injection lanes. |
| packaging or Dockerfile | image scan, SBOM smoke, reproducibility smoke. |
| docs only | docs link/build checks. |

The WS10 Dockerfile pins both the Go builder image and distroless non-root runtime image by digest. The pinned values are recorded in `.ci/versions.yaml`; release workflows must update both places together.

### Main Branch

Main should run all PR lanes plus:

- pinned OpenBao `2.5.3` CI e2e tests,
- pinned Kubernetes `1.34.3` Kind e2e,
- rotation tests,
- decrypt storm smoke,
- image scan,
- SBOM generation.

E2E lane selection should come from `test/e2e/suites.yaml`; concrete OpenBao and Kubernetes pins should continue to come from `.ci/versions.yaml`.

The OpenBao PR E2E lane should use the ephemeral CI target:

```sh
make test-e2e-openbao-ci
```

### Nightly

Nightly should run:

- full pinned `1.34.3` Kind e2e,
- OpenBao `2.5.3` CI e2e tests,
- failure injection,
- long-running Status polling,
- decrypt storm benchmark,
- supply-chain checks,
- optional kubeadm VM smoke when infrastructure exists.

### Release Gate

Release gate must run:

- all nightly lanes,
- kubeadm VM systemd test,
- kubeadm VM static pod test,
- OpenBao restore test,
- etcd/OpenBao paired restore test,
- upgrade and rollback test,
- image signing and provenance checks,
- byte reproducibility check,
- release evidence generation.

## Supply-Chain Gates

Required before publishing any release artifact:

- pinned GitHub Actions by commit SHA,
- vendored Go dependencies or an equivalent deterministic module policy,
- dependency review,
- license allowlist for shipped dependencies,
- static security scan,
- `govulncheck`,
- filesystem and image vulnerability scans,
- SBOM for each published binary/image,
- checksums for release assets,
- signed images and release checksums,
- provenance attestations,
- verification of attestations against expected workflow identity,
- byte reproducibility check for release images and SBOMs,
- release provenance index.

## Build Once, Promote By Digest

Release workflows should build immutable artifacts once, capture digests, verify trust evidence, and promote by digest.

Do not rebuild a different subject during publication.

```text
tag or release ref
  -> build immutable image/binary artifacts
  -> capture digests
  -> verify provenance and reproducibility
  -> sign and attest
  -> publish release assets and provenance index
```

## Release Channels

| Channel | Use | Support expectation |
|---|---|---|
| PR | validation only | no public artifacts |
| edge | main-branch integration signal | not production |
| nightly | scheduled drift detection | not production |
| prerelease | release candidate soak | staging/evaluation |
| stable | supported release line | only after release gates pass |

## Release Evidence

Every stable release should publish or retain:

- workflow run URL,
- source commit,
- signed tag,
- image digests,
- binary checksums,
- SBOMs,
- vulnerability scan result summary,
- provenance attestations,
- signature verification output,
- reproducibility report,
- release notes,
- compatibility matrix used for the release.

## Backlog Implications

The CI/supply-chain work is not a final polish task. It affects repository layout, Makefile targets, dependency policy, release artifact names, and version matrix ownership from the beginning.
