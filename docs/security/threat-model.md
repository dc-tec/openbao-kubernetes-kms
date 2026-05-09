# Threat Model

This document defines the initial security model for the OpenBao Kubernetes KMS provider.

## Assets

| Asset | Sensitivity |
|---|---|
| Kubernetes resource plaintext | High |
| KMS v2 plaintext request/response material | High |
| OpenBao Transit key material | Critical |
| OpenBao client token | High |
| JWT identity file | High |
| Plugin config | Medium to high |
| KMS Unix socket | High local control-plane access |
| Kubernetes `key_id` | Non-secret, security-relevant |
| KMS annotations | Non-secret, security-relevant |
| OpenBao audit logs | Sensitive metadata |
| etcd backups | High |
| OpenBao backups | Critical |

## Trust Boundaries

- `kube-apiserver` to local Unix socket.
- Plugin process to OpenBao HTTPS endpoint.
- OpenBao auth method to external JWT issuer.
- Plugin local filesystem to host users.
- OpenBao Transit policy to OpenBao administrators.
- etcd backup storage to backup operators.

## Expected Attacker Capabilities

The design should consider attackers who can:

- read etcd snapshots,
- read Kubernetes backups,
- observe plugin logs and metrics,
- access control-plane node files as a low-privilege user,
- submit malformed KMS requests through a compromised local API server path,
- cause OpenBao outages or network failures,
- steal stale JWTs or OpenBao tokens from disk/logs if present,
- modify non-root-readable configuration if file permissions are wrong.

The design does not defend against every action by:

- a fully compromised kube-apiserver process,
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
| JWT theft | File permissions, short lifetime, optional local issuer/audience/subject diagnostics, OpenBao role binding, external issuer. |
| Transit key deletion | `deletion_allowed=false`, no delete permission, tested backups. |
| Accidental key creation | `disable_upsert=true`, no create permission on encrypt path. |
| Key recreation with same name | Key lineage ID, decrypt validation, DR checks. |
| Ciphertext replay across clusters | AAD binds provider, cluster, OpenBao, key lineage, and version. |
| key_id spoofing | Strict local key ID registry and decrypt rejection before Transit. |
| Annotation tampering | Canonical AAD reconstruction and annotation hash checks. |
| KMS socket path replacement | Provider-owned, non-group-writable runtime directory; filesystem socket permissions; live-socket collision checks; verified-dead stale socket cleanup only. |
| Provider downgrade to plaintext | Remove `identity` fallback after migration; audit encryption config. |
| OpenBao MITM | TLS CA validation and server name verification. |
| OpenBao outage | Cached Status with staleness limits, fail closed, bootstrap grace, jittered auth retry backoff, alerting. |
| Log leakage | Redaction rules and tests for plaintext, JWT, tokens, ciphertext. |
| Metrics leakage | Hash key IDs and avoid raw OpenBao paths or high-cardinality labels. |
| Static pod API dependency | Static pod manifests avoid ConfigMaps, Secrets, ServiceAccounts. |

## Security Properties Provided

The design provides:

- confidentiality against offline etcd readers without OpenBao decrypt access,
- stronger rotation correctness through explicit Transit key version selection,
- deterministic, scoped, non-secret Kubernetes key IDs,
- metadata binding through Transit associated data,
- auditable OpenBao Transit operations,
- narrowed plugin permissions,
- reduced Kubernetes API circular dependency through JWT-first auth.

## Security Properties Not Provided

The design does not provide:

- protection from plaintext visible inside kube-apiserver during legitimate operation,
- protection from a compromised plugin process,
- protection from an attacker with Transit decrypt permission,
- protection from a malicious OpenBao administrator with destructive access,
- recovery after Transit key material is permanently lost,
- automatic encryption of all Kubernetes resources,
- encryption of etcd disk blocks, volumes, or node filesystems.

## Review Requirements

Before MVP:

- security review of key ID derivation,
- security review of AAD canonicalization,
- review of OpenBao policy,
- review of socket handling,
- review of JWT handling,
- review of log/metric redaction,
- failure-mode validation for key deletion, recreated keys, and premature `min_decryption_version`.

The implementation-backed `WS12-T07` through `WS12-T10` review evidence is recorded in [WS12 implementation security review](reviews/ws12-implementation-security-review.md).
