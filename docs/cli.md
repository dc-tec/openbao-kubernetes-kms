# CLI Reference

This document defines the intended command-line interface.

## serve

Start the KMS provider.

```sh
bao-kms-provider serve \
  --config /etc/openbao-kms/config.yaml
```

Responsibilities:

- validate config,
- authenticate to OpenBao,
- initialize active key snapshot,
- create Unix socket safely,
- serve KMS v2 gRPC,
- start background probes,
- expose health endpoints when configured.

Prometheus metrics are served on `server.metricsAddress` at `/metrics`.

## doctor

Run preflight checks.

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/encryption-config.yaml
```

Checks:

- config file permissions,
- socket path and parent directory,
- JWT file permissions and expiry,
- OpenBao TLS connection,
- JWT login,
- token policy,
- Transit key metadata,
- Transit key profile,
- `disable_upsert`,
- probe encrypt/decrypt,
- key ID determinism,
- Status/encrypt consistency,
- encryption config provider name,
- identity fallback status.

`doctor` must not print plaintext, JWTs, OpenBao tokens, or full ciphertext.
It prints a text report with stable check IDs and exits non-zero when any check fails.

## verify-key

Verify Transit key suitability.

```sh
bao-kms-provider verify-key \
  --config /etc/openbao-kms/config.yaml
```

Checks:

- key exists,
- key type allowed,
- export disabled,
- plaintext backup disabled,
- deletion disabled,
- derived/convergent settings match policy,
- latest version usable,
- `min_encryption_version` does not block active version,
- `min_decryption_version` does not block required historical versions.

## benchmark

Measure performance.

```sh
bao-kms-provider benchmark \
  --config /etc/openbao-kms/config.yaml \
  --iterations 5
```

Measures:

- Transit encrypt latency,
- Transit decrypt latency,
- non-secret Transit round-trip smoke behavior.

Output must redact sensitive data.
Expanded local KMS gRPC, decrypt storm, token lifecycle, and micro-batching comparisons are still release-gate benchmark work.

## rotation-plan

Report rotation state.

```sh
bao-kms-provider rotation-plan \
  --config /etc/openbao-kms/config.yaml
```

Reports:

- current active Transit version,
- current Kubernetes key ID hash,
- latest observed Transit version,
- pending promotion status,
- estimated promotion time,
- current `min_encryption_version`,
- current `min_decryption_version`,
- storage migration reminder,
- backup warnings.

## verify-rotation

Verify whether rotation migration is complete enough to proceed.

```sh
bao-kms-provider verify-rotation \
  --config /etc/openbao-kms/config.yaml
```

Possible strategies:

- inspect API server encryption migration status if available,
- scan Kubernetes resources through API server where possible,
- inspect etcd only in controlled administrative environments,
- compare plugin metrics and logs,
- report confidence level and limitations.

This command cannot prove absence of old ciphertext if it cannot inspect every encrypted resource and backup.

## Output Modes

Recommended flags:

```text
--output text
--output json
--log-level info
--redact=true
```

JSON output should be stable enough for automation once the CLI reaches beta.

## Exit Codes

Implemented stable exit codes:

| Code | Meaning |
|---:|---|
| 0 | success |
| 1 | unclassified command error |
| 2 | command usage error emitted by implemented command validation |
| 3 | configuration load or validation error |
| 4 | diagnostic/check failure |
| 5 | provider runtime failure |

## config

Inspect the typed configuration after defaults, file config, environment overrides, and supported root flag overrides are applied.

```sh
bao-kms-provider config \
  --config /etc/openbao-kms/config.yaml
```

The output includes the derived identity fingerprint when all identity-bearing fields are present.

## config schema

Print the configuration JSON Schema for documentation and tooling.

```sh
bao-kms-provider config schema
```

## policy openbao

Generate the least-privilege OpenBao policy for the configured Transit mount and key.

```sh
bao-kms-provider policy openbao \
  --config /etc/openbao-kms/config.yaml
```

The output grants only Transit metadata read, encrypt update, decrypt update, and `disable_upsert` inspection. Review the rendered paths before applying the policy.
