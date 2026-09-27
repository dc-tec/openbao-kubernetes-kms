---
title: EncryptionConfiguration
description: "Each EncryptionConfiguration field as the provider uses it, how automatic reload behaves, and what widening the resource list costs."
eyebrow: Reference
weight: 30
verifiedBy:
  - deploy/kubernetes/encryption-config.yaml
  - cmd/bao-kms-provider/diagnostics.go
---

The API server reads this file from `--encryption-provider-config`. The
maintained sample is `deploy/kubernetes/encryption-config.yaml`, and
[Enable encryption](/docs/get-started/enable-encryption/) walks through
installing it.

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

## Fields

| Field | Rule |
|---|---|
| `apiVersion` | `apiserver.config.k8s.io/v1`. |
| `providers[].kms.apiVersion` | `v2` for the `bao-kms-provider` entry. The provider does not implement v1. |
| `providers[].kms.name` | Equal to `transit.keyIdScope.providerName`. Identity-bearing: it feeds the [`key_id`](/docs/reference/key-id-and-aad/#key_id-format) and the [AAD envelope](/docs/reference/key-id-and-aad/#aad-envelope), and never changes after encryption begins. |
| `providers[].kms.endpoint` | `unix://` plus `server.socketPath`. Each API server connects to the provider on its own node. |
| `providers[].kms.timeout` | How long the API server waits for any KMS call. Start with `3s`, and lower it only to the measured p99 of `openbao_kms_grpc_duration_seconds` plus a margin, because startup decrypt storms and OpenBao failover raise tail latency. Too short a timeout blocks writes with `timeout` errors. |
| `resources[].resources` | The resource types encrypted on write. Start with `secrets`. |
| `identity: {}` | Keeps existing plaintext readable during migration. With `kms` first, writes never fall back to plaintext. |

`doctor --encryption-config` requires an entry with the configured provider name
and checks every such entry against the local KMS v2 API and socket. It warns
while `identity` remains.

## Migration files

`doctor` accepts files that combine this provider with `aescbc`, `aesgcm`,
`secretbox`, `identity`, or other KMS providers with their own names and
sockets. The parser also reads legacy KMS v1 entries, including `cachesize`,
without adding KMS v1 support to the provider.

During a staged migration the provider can follow an older one, but Kubernetes
writes with the first provider. Passing `doctor` shows the entry matches, not
that it comes first or that data has been migrated. The check rejects unknown
fields, entries with more than one provider type, a missing target provider,
and identity or endpoint mismatches. Local encryption keys need names and
secrets, whose values are redacted from errors. `doctor` does not check key
lengths or replace the API server's own validation.

## Preview installation scope

The preview guide targets fresh evaluation clusters. Keep `identity` second
after enabling KMS writes so existing plaintext objects remain readable.
Migration from an existing encryption configuration and removal of old readers
are deferred to stable-release planning. Passing `doctor` or checking a sample
does not establish that every stored object is encrypted.

## Automatic reload

With `--encryption-provider-config-automatic-reload=true`, the API server
applies a changed file immediately and keeps using it until the next valid
file. A mistyped provider name or an unreachable socket shows up only as KMS
Status failures and encrypt or decrypt errors on live traffic. Treat a reload
as a restart, not a safety check.

## Choose resources

Encryption is not retroactive: a resource type added later needs its objects
rewritten. Each added type raises KMS traffic on writes and cold reads, the
rewrite scope of every rotation, restore scope, startup decrypt load, and audit
scope. Add types one at a time, commonly `configmaps` next, and assess size and
traffic before encrypting custom resources.
