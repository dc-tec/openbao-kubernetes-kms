---
title: Threat model
description: "The assets, trust boundaries, and attackers the provider is designed around, the control for each threat, and what it does not protect."
eyebrow: Security · Fundamentals
weight: 10
verifiedBy:
  - internal/kmsv2
  - internal/aad
  - internal/socket
  - internal/logging
---

## Assets

| Asset | Sensitivity |
|---|---|
| OpenBao Transit key material and OpenBao backups | Critical |
| Kubernetes resource plaintext and KMS request and response material | High |
| OpenBao client token and provider auth material | High |
| KMS Unix socket | High (local control-plane access) |
| etcd backups | High |
| Provider configuration | Medium to high |
| Provider local state | Medium, security-relevant |
| Kubernetes `key_id` values and KMS annotations | Non-secret, security-relevant |
| OpenBao audit logs | Sensitive metadata |

## Trust boundaries

- `kube-apiserver` to the local Unix socket, and the runtime directory to the
  API server identity.
- The provider to the OpenBao HTTPS endpoint.
- OpenBao auth to the external JWT issuer or certificate authority.
- The provider's local files to other host users.
- The Transit policy to OpenBao administrators.
- etcd and OpenBao backup storage to backup operators.

Host administrators must protect the ancestor directories of socket, state,
and credential paths. Directory ownership and file checks do not isolate the
provider from root or another process running under the provider's UID.

## Attackers

The design considers attackers who can read etcd snapshots and Kubernetes
backups, observe provider logs and metrics, read control-plane files as a
low-privilege user, send malformed KMS requests through a compromised local API
server path, cause OpenBao or network outages, steal stale credentials if
controls fail, or modify configuration when file permissions are wrong.

## Threats and controls

| Threat | Control |
|---|---|
| Offline etcd snapshot theft | Encrypt selected resources before they reach etcd. |
| Local key exposure | Keep keys in OpenBao Transit, never on the host. |
| OpenBao token theft | Token kept in memory only, short TTLs, bounded renewal, never logged. |
| Auth material theft | File permissions, short credential lifetimes, identity-bound roles, an external issuer. |
| Transit key deletion | `deletion_allowed=false`, no delete permission for the provider, tested backups. |
| Accidental key creation | `disable_upsert=true` verified at runtime, no create permission for the provider. |
| Key recreated with the same name | Key lineage ID in AAD and decrypt validation. |
| Registry state rollback | State hash chain, adjacent checkpoint, monotonic generation, fail-closed startup. |
| Ciphertext replay across clusters, lineages, or providers | AAD binds provider, cluster, OpenBao instance, mount, lineage, and key version. |
| `key_id` spoofing or annotation tampering | Strict local registry and canonical AAD checks before any Transit call. |
| Socket path replacement | Provider-owned, non-group-writable runtime directory; stale sockets removed only when verified dead. |
| Downgrade to plaintext | Remove the `identity` fallback after migration and audit the `EncryptionConfiguration`. |
| Dependency on the protected API server | Auth without TokenReview; OpenBao outside the protected cluster's dependency path. |
| OpenBao impersonation | Pinned CA and server name verification. |
| Credentials forwarded by redirects | Every HTTP redirect is rejected before it is followed. |
| Oversized OpenBao responses | Per-operation response size limits. |
| KMS request flood | Separate concurrency limits for Status, Encrypt, and Decrypt; excess requests are rejected, never queued. |
| OpenBao outage | Cached Status with a staleness limit, fail-closed operations, bootstrap grace, jittered retries. |
| Compromised provider binary | Pinned, signed, attested, reproducible release artifacts and host hardening. Limited, because the provider sees plaintext in flight. |
| Log or metric leakage | Redaction rules with tests; hashed `key_id` labels; no raw paths or high-cardinality labels. |
| Static pod dependency on the API server | No ConfigMaps, Secrets, ServiceAccounts, or mounted tokens. |

## What the provider does not protect

- Plaintext inside `kube-apiserver` during normal operation, or a fully
  compromised API server.
- A compromised provider process or binary.
- Data from anyone who holds Transit decrypt permission, or from an OpenBao
  administrator with destructive authority.
- Rollback by a host administrator who replaces both the registry state and its
  checkpoint.
- Data after the Transit key material and all its backups are lost.
- Resources not listed in the `EncryptionConfiguration`, etcd disk blocks,
  application volumes, or node filesystems.
