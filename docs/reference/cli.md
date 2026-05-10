---
title: "CLI"
description: "Authoritative reference for the bao-kms-provider command-line interface: serve, doctor, verify-key, benchmark, rotation-plan, verify-rotation, config, policy openbao, exit codes."
weight: 10
---

# CLI

This page documents every command and flag supported by `bao-kms-provider` in v0.1. Commands print stable text output and use stable exit codes. They never print plaintext, JWTs, OpenBao tokens, or full ciphertext.

## serve

Start the KMS provider.

```sh
bao-kms-provider serve \
  --config /etc/openbao-kms/config.yaml
```

Responsibilities:

- validate config,
- authenticate to OpenBao,
- initialize the active key snapshot,
- create the Unix socket safely,
- serve KMS v2 gRPC,
- start background probes,
- expose health endpoints when configured.

Prometheus metrics are served on `server.metricsAddress` at `/metrics`. Health endpoints are served on `server.healthAddress`.

## doctor

Run preflight checks before promoting the binary or before changing the API server `EncryptionConfiguration`.

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/encryption-config.yaml
```

Checks:

| Check | Required behavior |
|---|---|
| OpenBao reachable | HTTPS connection succeeds with configured CA and SNI. |
| TLS valid | Certificate chain and server name validate. |
| JWT file readable | File exists, permissions are safe, content parses. |
| JWT expiry | Not expired and not within `auth.minJwtRemainingTtl`. |
| JWT claims | Issuer, audience, subject, and configured claims match expectations where locally checkable. |
| JWT login | OpenBao JWT login succeeds. |
| Token policy | Token can read Transit metadata and perform encrypt and decrypt. |
| Transit key exists | Metadata read succeeds. |
| Key type | Matches allowed key types. |
| Key export | `exportable=false` unless explicitly allowed. |
| Plaintext backup | `allow_plaintext_backup=false` unless explicitly allowed. |
| Key deletion | `deletion_allowed=false`. |
| Upsert | Transit mount has `disable_upsert=true` where configured. |
| Encryption/decryption | Test encrypt and decrypt of random non-secret probe data succeed. |
| Key ID generation | Deterministic across repeated runs. |
| Status/encrypt consistency | Local in-process test verifies `Status.key_id` equals `EncryptResponse.key_id`. |
| Socket path | Directory exists, has safe permissions, no unsafe stale path. |
| EncryptionConfiguration | Points to the socket, uses KMS v2, and the provider name matches. |
| Fallback | Warns if `identity` fallback remains enabled after migration. |

`doctor` prints a text report with stable check IDs and exits non-zero when any check fails.

## verify-key

Verify Transit key suitability against the recommended profile.

```sh
bao-kms-provider verify-key \
  --config /etc/openbao-kms/config.yaml
```

Checks:

- key exists,
- key type allowed,
- derived and convergent settings match policy,
- deletion disabled,
- export disabled,
- plaintext backup disabled,
- latest Transit version is usable,
- `min_encryption_version` does not block the active version,
- `min_decryption_version` does not block required historical versions.

## benchmark

Measure performance of the encrypt and decrypt path.

```sh
bao-kms-provider benchmark \
  --config /etc/openbao-kms/config.yaml \
  --iterations 5
```

Measures:

- Transit encrypt latency,
- Transit decrypt latency,
- non-secret Transit round-trip smoke behavior.

Benchmark output redacts sensitive data. Expanded local KMS gRPC, decrypt storm, token lifecycle, and micro-batching comparisons are release-gate work.

## rotation-plan

Report rotation state without performing rotation.

```sh
bao-kms-provider rotation-plan \
  --config /etc/openbao-kms/config.yaml
```

Reports:

- current active Transit version,
- current Kubernetes `key_id` hash,
- latest observed Transit version,
- pending promotion status,
- estimated promotion time,
- current `min_encryption_version`,
- current `min_decryption_version`,
- storage migration reminder,
- backup warnings.

## verify-rotation

Verify whether rotation migration has rewritten enough resources to proceed safely.

```sh
bao-kms-provider verify-rotation \
  --config /etc/openbao-kms/config.yaml
```

Strategies the command may use:

- inspect API server encryption migration status if available,
- scan Kubernetes resources through the API server where possible,
- inspect etcd only in controlled administrative environments,
- compare observed KMS key ID hashes from plugin metrics and logs.

This command cannot prove absence of old ciphertext if it cannot inspect every encrypted resource and backup. It reports the confidence level and the inspection coverage achieved.

## config

Inspect the typed configuration after defaults, file config, environment overrides, and supported root flag overrides have been applied.

```sh
bao-kms-provider config \
  --config /etc/openbao-kms/config.yaml
```

The output includes the derived identity fingerprint when all identity-bearing fields are present. See [Configuration: Identity-Bearing Fields](/reference/configuration/#identity-bearing-fields).

## config schema

Print the configuration JSON Schema for documentation and tooling.

```sh
bao-kms-provider config schema
```

The schema rejects unknown top-level and nested fields and reserves `configVersion: v1alpha1`.

## policy openbao

Generate the least-privilege OpenBao policy for the configured Transit mount and key.

```sh
bao-kms-provider policy openbao \
  --config /etc/openbao-kms/config.yaml
```

The output grants Transit metadata read, encrypt update, decrypt update, `disable_upsert` inspection, and `sys/capabilities-self` for `doctor` policy diagnostics. Review the rendered paths before applying the policy. See [Reference: Transit Policy Examples](/reference/transit-policy-examples/) for variants and rationale.

## Common Flags

Common flags supported across commands:

```text
--config <path>
--log-level trace|debug|info|warn|error
--metrics-address <host:port>
--health-address <host:port>
```

`--config` selects the provider configuration file. The other common flags override the corresponding configuration values for the current invocation.

Stable JSON output is not implemented yet. It remains tracked as a follow-up for automation consumers; text output is the supported CLI surface today.

## Exit Codes

| Code | Meaning |
|---:|---|
| 0 | success |
| 1 | unclassified command error |
| 2 | command usage error emitted by command validation |
| 3 | configuration load or validation error |
| 4 | diagnostic or check failure |
| 5 | provider runtime failure |
