---
title: "Compatibility"
description: "Tested Kubernetes, OpenBao, OS, deployment mode, Transit key type, and compatibility rules for bao-kms-provider."
weight: 80
---

# Compatibility

This matrix lists the versions and deployment shapes currently tested for
`bao-kms-provider`.

## Tested Preview Matrix

The preview matrix is intentionally narrow. A tagged release covers only the
versions, artifact families, and deployment models listed in its release notes
and in this matrix.

The initial preview matrix is:

- Kubernetes `1.34` and `1.35` release lines, with Kind node-image digests
  recorded in `.ci/versions.yaml`,
- Kubernetes KMS v2,
- OpenBao `2.6.0`,
- OpenBao Transit,
- JSON Web Token (JWT) auth in default release artifacts,
- certificate auth backed by a PKCS#11 hardware or software token only when the
  selected release includes matching opt-in artifacts and marks that path as
  tested,
- SPIFFE/SPIRE workload identity is not a supported preview user configuration,
- Linux control-plane nodes with filesystem Unix domain sockets.

## Kubernetes

| Version | Status |
|---|---|
| `< 1.29` | Not targeted. KMS v2 is the only implemented Kubernetes KMS API. |
| `1.29.x` through `1.33.x` | May work with KMS v2, but is not part of the tested preview matrix. |
| `1.34.3` | Tested Kind target pinned by node-image digest in `.ci/versions.yaml`. |
| `1.35.0` | Tested Kind target pinned by node-image digest in `.ci/versions.yaml`. |
| Other `1.34.x` or `1.35.x` patches | Candidate within the tested minor lines; covered only when listed by a release. |
| `1.36.x` | Intended next validation line once a digest-pinned Kind node image is available. Not tested yet. |

KMS v1 is not part of the primary implementation.

## OpenBao

| Version | Status |
|---|---|
| `2.6.0` | Current tested target. |
| Other `2.6.x` | Compatibility candidate; not claimed for this release until tested and pinned. |
| `2.5.x` | Not targeted for the current release line. |

The design requires OpenBao Transit features:

- a symmetric encryption key type such as `aes256-gcm96`,
- Transit encrypt and decrypt,
- explicit encrypt `key_version`,
- Transit `associated_data` for AEAD key types,
- key metadata including versions and restrictions,
- `min_encryption_version`,
- `min_decryption_version`,
- `disable_upsert`,
- JWT auth, or TLS certificate auth for cert-auth builds.

Certificate auth variants additionally require:

- OpenBao TLS certificate auth method,
- an OpenBao listener that requests client certificates,
- role constraints bound to the provider certificate identity,
- a PKCS#11 module for `certauth_pkcs11` builds.

## Operating Systems

Targeted:

- Linux control-plane nodes,
- filesystem Unix domain sockets,
- systemd or kubelet-managed static pod runtime.

Not targeted for the current release line:

- Windows control-plane nodes,
- abstract Unix sockets,
- non-Linux socket semantics.

## Deployment Modes

| Mode | Current status |
|---|---|
| systemd | Targeted. |
| Static pod | Targeted. |
| DaemonSet | Not recommended for protecting the same cluster's API server. |
| Sidecar with `kube-apiserver` | Not targeted. |

See [Deployment: Choosing A Model](/deployment/choosing-a-model/) for the model selection rationale.

## Transit Key Types

| Key type | Status |
|---|---|
| `aes256-gcm96` | Supported and recommended default. |
| Other AEAD Transit key types | Not supported. |
| Derived or convergent keys | Not supported for the Kubernetes KMS path. |

## Transit Profile Findings

Transit profile findings are fail-closed. When metadata shows a blocking
profile issue, the provider marks readiness and KMS Status unhealthy instead of
encrypting with settings outside the validated contract.

Findings are classified by impact:

- `cryptographic_safety`: settings that weaken or change the validated
  encryption and additional authenticated data (AAD) contract, such as an
  unsupported key type, exportable key material, plaintext backup, derived
  mode, or convergent encryption.
- `api_server_availability`: settings that can strand API server reads or
  writes, such as key deletion, unsupported encrypt/decrypt operations, or
  version restrictions that block active or historical versions.

Fail-closed behavior protects the cryptographic contract. It does not guarantee
API server write availability while OpenBao is misconfigured, sealed,
unreachable, or changed to an unsafe Transit profile. Use `verify-key`,
`doctor`, readiness, and KMS Status as preflight and monitoring signals before
changing API server encryption settings.

## Auth Methods

| Auth method | Build | Status |
|---|---|---|
| JWT | default release artifacts | Supported preview path. |
| Certificate with PKCS#11 source | `certauth_pkcs11` opt-in host artifact | Opt-in preview path only when the selected release publishes the PKCS#11 artifact and marks it as tested. |
| Certificate with SPIFFE source | `certauth_spiffe` local-only artifact | Not a supported preview user configuration. |
| OpenBao Kubernetes auth | any | Not supported because TokenReview depends on the protected API server. |

## Compatibility Promises

After the first stable release, these surfaces remain backward compatible within a major version:

- `key_id` derivation for existing epochs,
- Unix-second Transit version creation-time normalization used by `key_id`,
- annotation schema,
- AAD canonicalization,
- configuration field meanings for identity-bearing values,
- decrypt support for historical `key_id` values,
- CLI JSON report shape for report-style commands.

## CI Version Policy

CI does not use floating `latest` inputs for the tested compatibility matrix.

The implementation uses a central version manifest at `.ci/versions.yaml` for:

- OpenBao image tag and digest,
- Kubernetes exact patch versions,
- Kind node image digest,
- release matrix rows,
- intended next validation lines and future candidate versions.

For the full CI and supply-chain controls see [Development: CI And Supply Chain](/development/ci-supply-chain/).

## Breaking Changes

Breaking changes require:

- a written design decision,
- a migration guide,
- a release note,
- updated test fixtures,
- an explicit compatibility section in the release notes.

Examples of breaking changes:

- changing `key_id` derivation,
- changing AAD canonicalization,
- dropping a historical annotation version,
- changing the default AAD mode,
- changing provider-name handling,
- removing decrypt support for retained historical `key_id` values.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao JWT/OIDC auth API](https://openbao.org/api-docs/auth/jwt/)
- [OpenBao TLS certificates auth method](https://openbao.org/docs/auth/cert/)
- [SPIFFE Workload API](https://spiffe.io/docs/latest/spiffe-specs/spiffe_workload_api/)
