---
title: "Threat Model"
description: "Assets, trust boundaries, attacker capabilities, threats and controls, security properties provided and not provided by bao-kms-provider."
weight: 10
---

# Threat Model

This page is the threat model for `bao-kms-provider`. It enumerates the assets, the trust boundaries the provider sits across, the attacker capabilities the design considers, and the security properties the design provides and does not provide.

## Assets

| Asset | Sensitivity |
|---|---|
| Kubernetes resource plaintext | High |
| KMS v2 plaintext request and response material | High |
| OpenBao Transit key material | Critical |
| OpenBao client token | High |
| Provider auth material | High |
| Plugin configuration | Medium to high |
| Plugin local state | Medium, security-relevant |
| KMS Unix socket | High local control-plane access |
| Kubernetes `key_id` values | Non-secret, security-relevant |
| KMS annotations | Non-secret, security-relevant |
| OpenBao audit logs | Sensitive metadata |
| etcd backups | High |
| OpenBao backups | Critical |

## Trust Boundaries

- `kube-apiserver` to the local Unix socket.
- Plugin process to the OpenBao HTTPS endpoint.
- OpenBao auth method to the external JWT issuer or certificate authority.
- Plugin local filesystem to host users.
- Plugin runtime directory to the API server identity.
- OpenBao Transit policy to OpenBao administrators.
- etcd backup storage to backup operators.
- OpenBao backup storage to backup operators.

## Expected Attacker Capabilities

The design considers attackers who can:

- read etcd snapshots,
- read Kubernetes backups,
- observe plugin logs and metrics,
- access control-plane node files as a low-privilege user,
- submit malformed KMS requests through a compromised local API server path,
- cause OpenBao outages or network failures,
- steal stale JWTs, certificate PIN files, or OpenBao tokens if controls fail,
- modify configuration files when file permissions are wrong.

The design does not defend against every action by:

- a fully compromised `kube-apiserver` process,
- a malicious plugin binary,
- an OpenBao administrator with destructive authority,
- an attacker with valid Transit decrypt permission,
- loss of all Transit key backups.

## Threats And Controls

| Threat | Control |
|---|---|
| Offline etcd snapshot theft | Encrypt selected API resources before persistence. |
| Local key exposure | Use remote OpenBao Transit instead of static local encryption keys. |
| OpenBao token theft | Memory-only token storage, short TTLs, explicit renewal increment, no token logs. |
| Auth material theft | File permissions, short JWT and certificate lifetimes, claim or certificate identity binding, and an external issuer where feasible. |
| Transit key deletion | `deletion_allowed=false`, no delete permission for the plugin token, tested backups. |
| Accidental key creation | `disable_upsert=true` at the Transit mount, no create permission for the plugin token. |
| Key recreation with same name | Key lineage ID, decrypt validation, DR checks. |
| Registry state rollback | State hash chain, adjacent checkpoint, monotonic generation checks, and fail-closed startup when the checkpoint survives. |
| Ciphertext replay across clusters | AAD binds provider, cluster, OpenBao instance, key lineage, and key version. |
| `key_id` spoofing | Strict local key registry and decrypt rejection before Transit. |
| Annotation tampering | Canonical AAD reconstruction and annotation hash checks. |
| KMS socket path replacement | Provider-owned, non-group-writable runtime directory; filesystem socket permissions; live-socket collision checks; verified-dead stale socket cleanup only. |
| Provider downgrade to plaintext | Remove `identity` fallback after migration; audit `EncryptionConfiguration`. |
| Protected API server dependency loop | Use provider auth without TokenReview and keep OpenBao outside the protected API-server dependency path. |
| OpenBao MITM | TLS CA validation and server name verification. |
| OpenBao outage | Cached Status with staleness limits, fail closed, bootstrap grace, jittered auth retry backoff, alerting. |
| Malicious or compromised plugin binary | Host hardening, pinned release artifacts, and signing, reproducibility, and attestation evidence before a production-ready claim. Defense is limited because the plugin sees KMS plaintext material in flight. |
| Log leakage | Redaction rules and tests for plaintext, JWT, tokens, and ciphertext. |
| Metrics leakage | Hashed `key_id` values; raw OpenBao paths and high-cardinality labels excluded. |
| Static pod API dependency | Static pod manifests avoid ConfigMaps, Secrets, ServiceAccounts, and mounted service account tokens. |

## Security Properties Provided

The design provides:

- confidentiality against offline etcd readers without OpenBao decrypt access,
- stronger rotation correctness through explicit Transit key version selection on every encrypt,
- deterministic, scoped, non-secret Kubernetes `key_id` values,
- metadata binding through Transit associated data; see [AAD And Decrypt Validation](/security/aad-and-decrypt-validation/),
- auditable OpenBao Transit operations,
- narrowed plugin permissions,
- reduced Kubernetes API circular dependency through provider authentication that avoids TokenReview; see [Auth Model](/security/auth-model/).

## Security Properties Not Provided

The design does not provide:

- protection from plaintext visible inside `kube-apiserver` during legitimate operation,
- protection from a compromised plugin process,
- tamper-proof rollback protection if a host-level attacker can replace both local registry state and checkpoint,
- protection from an attacker with Transit decrypt permission,
- protection from an OpenBao administrator with destructive access,
- recovery after Transit key material is permanently lost,
- automatic encryption of all Kubernetes resources,
- encryption of etcd disk blocks, application volumes, or node filesystems.

## Review Requirements

Before any public release:

- security review of `key_id` derivation,
- security review of AAD canonicalization,
- review of OpenBao policy,
- review of socket handling,
- review of auth material handling,
- review of log and metric redaction,
- failure-mode validation for key deletion, recreated keys, and premature `min_decryption_version`.

Implementation-backed security review evidence is part of the release evidence set and must be reviewed before a production-ready claim.
