# Compatibility

This document defines compatibility policy. Do not expand support claims until CI and release tests prove them.

## Current Claim

Status: no tested compatibility claims yet.

The v0.1 design target is:

- Kubernetes 1.34 release line,
- Kubernetes KMS v2,
- OpenBao 2.5.3,
- OpenBao Transit,
- JWT auth,
- Linux control-plane nodes with filesystem Unix domain sockets.

## Kubernetes

Validation baseline:

| Version | Status |
|---|---|
| `< 1.34` | Not targeted for v0.1. |
| `1.34.x` | Initial v0.1 validation line. Exact patch and Kind image digest must be pinned in CI. |
| `1.35.x` | Future candidate. Not a v0.1 support claim until release-gated. |
| `1.36.x` | Future candidate. Not a v0.1 support claim until release-gated. |

KMS v1 is not part of the primary implementation.

## OpenBao

Initial OpenBao validation target:

| Version | Status |
|---|---|
| `2.5.3` | Initial v0.1 validation target. |
| other `2.5.x` | Future compatibility candidate; not claimed until tested. |
| `2.4.x` | Not targeted for v0.1. |

The design requires OpenBao Transit features:

- symmetric encryption key type such as `aes256-gcm96`,
- Transit encrypt/decrypt,
- explicit encrypt `key_version`,
- Transit associated data for AEAD key types,
- key metadata including versions and restrictions,
- `min_encryption_version`,
- `min_decryption_version`,
- `disable_upsert`,
- JWT auth.

## Operating Systems

Target:

- Linux control-plane nodes,
- filesystem Unix domain sockets,
- systemd or kubelet/static pod runtime.

Not targeted for MVP:

- Windows control-plane nodes,
- abstract Unix sockets,
- non-Linux socket semantics.

## Deployment Modes

| Mode | MVP status |
|---|---|
| systemd | Targeted. |
| static pod | Targeted. |
| DaemonSet | Not recommended for protecting the same cluster API server. |
| sidecar with kube-apiserver | Not targeted. |

## Transit Key Types

| Key type | Status |
|---|---|
| `aes256-gcm96` | Recommended default. |
| `xchacha20-poly1305` | Optional after testing. |
| derived/convergent keys | Not recommended. |

## Compatibility Promises

After first release, these should remain backward compatible within a major version:

- key ID derivation for existing epochs,
- annotation schema,
- AAD canonicalization,
- config field meanings for identity-bearing values,
- decrypt support for historical key IDs,
- CLI JSON output once marked stable.

## CI Version Policy

CI must not use floating `latest` inputs for compatibility claims.

The implementation should use a central version manifest for:

- OpenBao image tag and digest,
- Kubernetes exact patch version,
- Kind node image digest,
- release-gate matrix rows,
- future candidate versions.

See [CI and supply chain](ci-supply-chain.md).

## Breaking Changes

Breaking changes require:

- ADR,
- migration guide,
- release note,
- test fixture update,
- explicit compatibility section.

Examples:

- changing key ID derivation,
- changing AAD canonicalization,
- dropping a historical annotation version,
- changing default AAD mode,
- changing provider-name handling,
- removing decrypt support for old key epochs.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao JWT auth](https://openbao.org/docs/2.4.x/auth/jwt/)
