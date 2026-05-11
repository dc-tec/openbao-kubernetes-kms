---
title: "Metrics"
description: "Authoritative metric and log-field reference: every Prometheus metric exported by bao-kms-provider and every stable log field name."
weight: 40
---

# Metrics

This page is the authoritative reference for the Prometheus metrics and stable JSON log fields exported by `bao-kms-provider`. For observability principles, error classes, health endpoints, and alerts, see [Reference: Observability](/reference/observability/).

## Endpoint

Prometheus metrics are served by `bao-kms-provider serve` on `server.metricsAddress` at `/metrics`. The default address is `127.0.0.1:8081`, so the endpoint is not reachable from off-host without explicit configuration.

## Label Rules

The provider applies these rules to every metric:

- bounded label values only,
- `key_id` values are exported as hashes, never raw,
- raw OpenBao paths are not exported,
- raw key names are not exported,
- Kubernetes object names are not used as labels,
- request UIDs are not used as labels,
- full error strings are not used as labels.

## Metric Reference

### KMS gRPC Surface

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_grpc_requests_total` | counter | `method`, `status` | KMS v2 gRPC method invocations and outcomes (`Status`, `Encrypt`, `Decrypt`). |
| `openbao_kms_grpc_duration_seconds` | histogram | `method` | Per-method latency for KMS v2 gRPC handlers. |

### OpenBao Calls

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_openbao_requests_total` | counter | `operation`, `status` | OpenBao API call counts by operation (`auth.login`, `auth.renew`, `transit.encrypt`, `transit.decrypt`, `transit.metadata`) and outcome. |
| `openbao_kms_openbao_duration_seconds` | histogram | `operation` | Per-operation latency for OpenBao calls. |

### Auth And Token

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_auth_login_total` | counter | `status` | JWT login attempts by outcome. |
| `openbao_kms_auth_renewal_total` | counter | `status` | Token renewal attempts by outcome. |
| `openbao_kms_token_ttl_seconds` | gauge | none | Remaining TTL of the current OpenBao token. |

### Active Key

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_status_key_id_hash` | gauge | `hash` | Reports `1` for the current Kubernetes `key_id` hash. The label value is the `base64url-sha256` of the active `key_id`. |
| `openbao_kms_key_version` | gauge | none | Active OpenBao Transit key version used for new encrypt operations. |

### Status And Probes

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_status_cache_age_seconds` | gauge | none | Age of the cached KMS Status response. |
| `openbao_kms_transit_metadata_observation_total` | counter | `status` | Background Transit metadata probe outcomes. |

### Rotation

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_rotation_state` | gauge | `state` | Reports `1` for the current rotation state (`ObservedOld`, `NewVersionObserved`, `PendingStability`, `PendingActivationDelay`, `Active`, `Rejected`). |

### Validation Errors

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_aad_validation_errors_total` | counter | `reason` | AAD validation failures during decrypt. |
| `openbao_kms_decrypt_key_id_errors_total` | counter | `reason` | Decrypt rejections caused by unknown, malformed, or stale-disallowed `key_id`. |

### Performance

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_decrypt_batch_size` | histogram | none | Reserved decrypt micro-batch sizes; not emitted while v0.1 rejects batching enablement. |

### Runtime Health

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_circuit_breaker_state` | gauge | none | OpenBao client circuit breaker state. |
| `openbao_kms_socket_restarts_total` | counter | none | Socket reclamations after stale socket detection. |

## Log Fields

Stable JSON log fields. Operators can rely on these names across v0.1 patch releases.

| Field | Type | Description |
|---|---|---|
| `ts` | string | RFC 3339 timestamp. |
| `level` | string | Log level (`debug`, `info`, `warn`, `error`). |
| `operation` | string | Logical operation name (`kms.encrypt`, `kms.decrypt`, `kms.status`, `openbao.encrypt`, `openbao.decrypt`, `openbao.metadata`, `auth.login`, `auth.renew`). |
| `status` | string | Operation outcome (`ok`, `error`). |
| `duration_ms` | number | Operation latency in milliseconds. |
| `key_id_hash` | string | `base64url-sha256` of the active `key_id`. Never the raw `key_id`. |
| `transit_key_version` | number | OpenBao Transit key version associated with the operation. |
| `openbao_request_duration_ms` | number | Latency of the underlying OpenBao call, when applicable. |
| `openbao_request_id` | string | OpenBao request ID when debug correlation is enabled and OpenBao returned a safe ID. |
| `error_class` | string | One of the stable error classes; see [Observability: Error Classes](/reference/observability/#error-classes). |
| `request_uid_hash` | string | Hash of the KMS request UID when debug correlation is enabled. |
| `debug_correlation_incident` | string | Operator-supplied incident ID when debug correlation is enabled. |
| `debug_correlation_expires_at` | string | RFC 3339 timestamp at which debug correlation will expire. |

The provider must never include `plaintext`, `jwt`, `openbao_token`, `ciphertext`, `transit_key_material`, raw `openbao_path`, or raw `key_name` fields in any log entry.
