# 0010: Project Naming And M0 Foundation

## Status

Accepted.

## Context

The repository was renamed before implementation starts. M0 needs stable names and foundation tooling so module paths, binary names, service units, release artifacts, CI policy, and documentation do not drift after code lands.

## Decision

- Project and repository name: `openbao-kubernetes-kms`.
- Go module path: `github.com/dc-tec/openbao-kubernetes-kms`.
- Binary name: `bao-kms-provider`.
- systemd unit name: `bao-kms-provider.service`.
- Container image name: `bao-kms-provider`.
- Kubernetes provider names remain deployment-specific, for example `openbao-kms-workload-a`.
- Host paths remain OpenBao/KMS explicit, for example `/etc/openbao-kms` and `/run/openbao-kms/kms.sock`.
- Key ID domain separator uses the project identity: `openbao-kubernetes-kms/key-id/v1`.
- Go toolchain version: `1.26.3`.
- CLI/config framework: Viper.
- Local task runner: Makefile.
- Central version policy lives at `.ci/versions.yaml`.

## Consequences

- CLI and docs use `bao-kms-provider`.
- Operators still see explicit OpenBao KMS paths during incidents and runbooks.
- M0 must initialize `go.mod` with the accepted module path and toolchain.
- CI and release workflows must read validation versions from `.ci/versions.yaml` instead of duplicating them.
- Exact Kubernetes Kind validation is pinned in `.ci/versions.yaml`; the initial Kind lane uses `kindest/node:v1.34.3` by digest while tracking upstream `1.34.7` as the latest patch.
