---
title: "Compatibility"
description: "Kubernetes, OpenBao, OS, deployment mode, Transit key type, and CI version policy supported by bao-kms-provider, plus breaking-change rules."
weight: 80
---

# Compatibility

This page defines the compatibility matrix for `bao-kms-provider`. Support claims do not expand beyond what CI and release tests prove.

## Current Claim

The support envelope is intentionally narrow. A tagged release claims only the exact versions and deployment lanes recorded in that release's evidence bundle.

The initial public release envelope is:

- Kubernetes `1.34` release line, with exact patch pins recorded in `.ci/versions.yaml`,
- Kubernetes KMS v2,
- OpenBao `2.5.3`,
- OpenBao Transit,
- JWT auth in default builds,
- certificate auth implementation in build-tagged PKCS#11 and SPIFFE variants, with release support pending provider source E2E and release artifact evidence,
- Linux control-plane nodes with filesystem Unix domain sockets.

## Kubernetes

| Version | Status |
|---|---|
| `< 1.34` | Not targeted for the current release line. |
| `1.34.3` | Initial Kind e2e target pinned by node-image digest in `.ci/versions.yaml`. |
| Other `1.34.x` patches | Candidate within the same release line; claimed only after an exact-pinned lane exists in release evidence. |
| `1.35.x` | Future candidate. Not a support claim until release evidence exists. |
| `1.36.x` | Future candidate. Not a support claim until release evidence exists. |

KMS v1 is not part of the primary implementation.

## OpenBao

| Version | Status |
|---|---|
| `2.5.3` | Initial validation target. |
| Other `2.5.x` | Future compatibility candidate; not claimed until tested. |
| `2.4.x` | Not targeted for the current release line. |

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
- a PKCS#11 module for `certauth_pkcs11` builds or a SPIFFE Workload API socket for `certauth_spiffe` builds.

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

## Auth Methods

| Auth method | Build | Status |
|---|---|---|
| JWT | default | Supported. |
| Certificate with PKCS#11 source | `certauth_pkcs11` | Implemented. Release support is pending host artifact evidence plus provider source E2E evidence with a compatible PKCS#11 module and token. |
| Certificate with SPIFFE source | `certauth_spiffe` | Implemented. Release support is pending SPIFFE artifact evidence plus provider source E2E evidence with an X.509 SVID from the SPIFFE Workload API. |
| OpenBao Kubernetes auth | any | Not supported because TokenReview depends on the protected API server. |

## Compatibility Promises

After the first stable release, these surfaces remain backward compatible within a major version:

- `key_id` derivation for existing epochs,
- annotation schema,
- AAD canonicalization,
- configuration field meanings for identity-bearing values,
- decrypt support for historical `key_id` values,
- CLI JSON report shape for report-style commands.

## CI Version Policy

CI does not use floating `latest` inputs for compatibility claims.

The implementation uses a central version manifest at `.ci/versions.yaml` for:

- OpenBao image tag and digest,
- Kubernetes exact patch version,
- Kind node image digest,
- release matrix rows,
- future candidate versions.

For the full CI and supply-chain controls see [Development: CI And Supply Chain](/development/ci-supply-chain/).

## Breaking Changes

Breaking changes require:

- a written design decision,
- a migration guide,
- a release note,
- updated test fixtures,
- an explicit compatibility section in the release evidence.

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
