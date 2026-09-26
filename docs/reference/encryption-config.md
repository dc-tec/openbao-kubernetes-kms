---
title: "EncryptionConfiguration"
description: "Authoritative reference for the Kubernetes API server EncryptionConfiguration shape used with bao-kms-provider: required fields, semantics, automatic reload caveats, and resource selection."
weight: 50
---

This reference defines the Kubernetes API server `EncryptionConfiguration` shape used with `bao-kms-provider`. For the bring-up tutorial, see [Getting Started: Kubernetes Encryption Config](/docs/getting-started/kubernetes-encryption-config/).

## Minimal Shape

```yaml
apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
    providers:
      - kms:
          apiVersion: v2
          name: openbao-kms-workload-a
          endpoint: unix:///run/openbao-kms/kms.sock
          timeout: 3s
      - identity: {}
```

A maintained sample lives at `deploy/kubernetes/encryption-config.yaml` in the repository.

## Field Reference

### Top-Level `apiVersion`

Always `apiserver.config.k8s.io/v1`; this is the Kubernetes API server configuration object version.

### `providers[].kms.apiVersion`

Use `v2` for the `bao-kms-provider` entry. This provider does not implement KMS v1.

### `name`

Identity-bearing. The value:

- must match `transit.keyIdScope.providerName` in the provider configuration,
- participates in `key_id` derivation; see [Reference: Key ID And AAD](/docs/reference/key-id-and-aad/#recommended-format),
- participates in additional authenticated data (AAD) envelope construction; see [Reference: Key ID And AAD](/docs/reference/key-id-and-aad/#aad-envelope),
- must not change after encryption begins without a documented migration plan.

`doctor` requires an entry with the configured provider name. It validates every
entry with that name against the local KMS v2 API and socket configuration; see
[Reference: CLI: doctor](/docs/reference/cli/#doctor).

### `endpoint`

The endpoint must use the `unix://` scheme and must match `server.socketPath` in the provider configuration. Other endpoint forms are rejected.

Every control-plane node must have a local provider instance serving the same endpoint path. The API server does not connect across hosts.

### `timeout`

The duration the API server waits for any KMS gRPC call before treating it as failed.

Initial recommendation: `3s`.

Tighten this value only after benchmark and failure-mode testing. The provider
targets much lower normal latency. Startup decrypt storms and OpenBao failover
can still produce tail latency that approaches the timeout. Set the timeout
against measured p99 of `openbao_kms_grpc_duration_seconds` plus a safety
margin, not against the steady-state median.

A timeout that is too short surfaces as `timeout` errors in the [error class catalog](/docs/reference/observability/#error-classes) and may block writes to encrypted resources.

### `resources`

The list of API resources the provider encrypts at write time. Resources outside this list remain unencrypted in etcd.

Recommended starting point:

```yaml
resources:
  - secrets
```

Common second step is to add `configmaps`. CRDs can be encrypted with the same provider once the operator has assessed size, read and write volume, and recovery impact.

The `resources` set is not retroactive. Adding a resource type after encryption begins requires a storage migration; see [Getting Started: Migrate Existing Resources](/docs/getting-started/kubernetes-encryption-config/#migrate-existing-resources).

### `identity` Fallback

The `identity` provider is the API server's no-op fallback. With it last in the `providers` list:

- new writes go through `kms`,
- existing plaintext objects remain readable,
- `kms` failures do not silently fall back to plaintext writes (Kubernetes does not silently downgrade between providers when `kms` is first).

Remove `identity` after every targeted resource has been rewritten through `kms`. Leaving it in place indefinitely increases the chance that future misconfiguration produces plaintext writes; removing it too early breaks reads of plaintext objects that were not migrated. See [Getting Started: Remove The Identity Fallback](/docs/getting-started/kubernetes-encryption-config/#remove-the-identity-fallback).

## Migration Files

`doctor --encryption-config` accepts files that combine this provider with
`aescbc`, `aesgcm`, `secretbox`, `identity`, or other KMS providers. Additional
KMS providers can have different names and Unix socket paths. The parser also
accepts legacy KMS v1 entries, including `cachesize`; this does not add KMS v1
support to `bao-kms-provider` or establish support in your Kubernetes version.

The configured provider can appear after an old provider during a staged
migration. Kubernetes uses the first provider for new writes. A passing doctor
check establishes that the configured provider entry matches, not that it is
first or that stored data has been migrated.

The check rejects unknown fields, multiple provider types in one entry, missing
target providers, and target identity or endpoint mismatches. Local encryption
keys must have names and secrets. Their values are redacted from parse errors
and diagnostic formatting. The check does not validate their cryptographic
lengths or replace Kubernetes configuration validation.

## Automatic Reload

Kubernetes supports automatic reload of the encryption provider configuration when `kube-apiserver` is started with:

```text
--encryption-provider-config-automatic-reload=true
```

Reload caveats:

- The API server applies the new configuration immediately and surfaces any errors at the next encrypt or decrypt call rather than during validation.
- Provider name typos, unreachable sockets, or socket permission changes surface as KMS Status failures and ultimately as encrypt or decrypt errors against live traffic.
- A failed reload does not roll back to the previous configuration. Once a configuration is applied, the API server uses it until the next valid configuration is seen.

Treat reload as a faster restart, not a safety check.

## Resource Selection Trade-Offs

Adding a resource type to the `resources` list increases:

- KMS provider request volume during writes,
- decrypt traffic during reads (mitigated by API server caches),
- migration scope when rotating keys,
- recovery scope when restoring from backup,
- latency impact during startup decrypt storms,
- audit and compliance scope.

Plan the resource set deliberately. Encrypting all resources is rarely the right starting point.

## Source References

- [Kubernetes encryption at rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
