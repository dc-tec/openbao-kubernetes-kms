---
title: "CLI"
description: "Authoritative reference for the bao-kms-provider command-line interface: serve, doctor, verify-key, benchmark, rotation-plan, verify-rotation, config, policy openbao, exit codes."
weight: 10
---

# CLI

This page documents every command and flag supported by `bao-kms-provider`.
Commands print stable text or JSON output where documented and use stable exit
codes. They never print plaintext, JWTs, OpenBao tokens, or full ciphertext.

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
  --encryption-config /etc/kubernetes/encryption-config.yaml \
  --output json
```

Checks:

| Check | Required behavior |
|---|---|
| OpenBao reachable | HTTPS connection succeeds with configured CA and SNI. |
| TLS valid | Certificate chain and server name validate. |
| Local auth material | For JWT auth, the JWT file exists, permissions are safe, content parses, expiry is acceptable, and configured claims match. For cert auth, the configured certificate source must be reachable, the current certificate must be locally valid, and the signer must match the certificate and accept a non-secret probe. |
| OpenBao auth login | The configured OpenBao auth method login succeeds. |
| Token policy | Token can read Transit metadata and perform encrypt and decrypt. |
| Transit key exists | Metadata read succeeds. |
| Key type | Matches allowed key types. |
| Key export | `exportable=false`. |
| Plaintext backup | `allow_plaintext_backup=false`. |
| Key deletion | `deletion_allowed=false`. |
| Upsert | Transit mount has `disable_upsert=true` where configured. |
| Encryption/decryption | Test encrypt and decrypt of random non-secret probe data succeed. |
| Key ID generation | Deterministic across repeated runs. |
| Status/encrypt consistency | Local in-process test verifies `Status.key_id` equals `EncryptResponse.key_id`. |
| Socket path | Directory exists, has safe permissions, no unsafe stale path. |
| EncryptionConfiguration | Points to the socket, uses KMS v2, and the provider name matches. |
| Fallback | Warns if `identity` fallback remains enabled after migration. |

`doctor` prints a report with stable check IDs and exits non-zero when any
check fails. Use `--output text` for the default human-readable report or
`--output json` for automation.

Transit profile failures include an impact prefix. `cryptographic_safety`
findings protect the validated encryption and AAD contract. `api_server_availability`
findings identify settings that can make Kubernetes reads or writes fail even
though they may not weaken ciphertext confidentiality directly.

## verify-key

Verify Transit key suitability against the recommended profile.

```sh
bao-kms-provider verify-key \
  --config /etc/openbao-kms/config.yaml \
  --output json
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

Benchmark output redacts sensitive data. Expanded local KMS gRPC, decrypt storm, token lifecycle, and micro-batching comparisons are release-validation work.

## rotation-plan

Report rotation state without performing rotation.

```sh
bao-kms-provider rotation-plan \
  --config /etc/openbao-kms/config.yaml \
  --output json
```

Reports:

- whether local registry state was loaded,
- registry generation and state hash when available,
- registry checkpoint status, generation, and hash when available,
- state bootstrap eligibility and reason when local registry state is absent,
- current active Transit version,
- current Kubernetes `key_id` hash,
- latest observed Transit version,
- pending promotion status,
- estimated promotion time when a pending version has become stable.

Checkpoint status values:

- `current`: checkpoint exists and matches the loaded state generation/hash,
- `behind`: checkpoint exists and accepts a newer state generation,
- `missing`: state exists but the checkpoint is absent,
- `absent`: neither state nor checkpoint exists.

If local registry state is missing, `rotation-plan` only synthesizes initial
bootstrap state from initial Transit metadata. It fails instead of reporting an
active key hash from live Transit metadata after rotation. If a checkpoint is
present but the state file is missing or rolled back, the command fails closed.
When local state is absent and OpenBao metadata is readable, the command reports
or returns the exact auto-bootstrap reason, such as an advanced
`latest_version`, `min_available_version`, or `min_decryption_version`.

## verify-rotation

Report local rotation preflight state.

```sh
bao-kms-provider verify-rotation \
  --config /etc/openbao-kms/config.yaml \
  --output json
```

The command reports the same local registry and Transit metadata view as
`rotation-plan`, plus `confidence: limited` and a `limitations` field. It does
not scan Kubernetes resources, inspect etcd, prove that every encrypted resource
or retained backup has been rewritten, or recommend raising OpenBao
`min_decryption_version`.

Treat it as a local preflight signal. The operator still owns independent
migration evidence, backup-retention evidence, and any change to
`min_decryption_version`.

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

## version

Print build metadata for the running binary.

```sh
bao-kms-provider version
```

Output fields:

- `version`
- `commit`
- `buildDate`
- `dirty`

Use this command when comparing control-plane nodes during upgrades, rollback checks, and incident response.

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

Report-style commands also support:

```text
--output text|json
```

`doctor`, `verify-key`, `rotation-plan`, and `verify-rotation` support stable
JSON reports for automation consumers. `text` remains the default.

## Exit Codes

| Code | Meaning |
|---:|---|
| 0 | success |
| 1 | unclassified command error |
| 2 | command usage error emitted by command validation |
| 3 | configuration load or validation error |
| 4 | diagnostic or check failure |
| 5 | provider runtime failure |
