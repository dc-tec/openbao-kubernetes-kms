---
title: "Metrics"
description: "Authoritative metric and log-field reference: every Prometheus metric exported by bao-kms-provider and every stable log field name."
weight: 40
---

# Metrics

This reference defines the Prometheus metrics and stable JSON log fields exported by `bao-kms-provider`. For observability principles, error classes, health endpoints, and alerts, see [Reference: Observability](/reference/observability/).

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
| `openbao_kms_grpc_requests_total` | counter | `method`, `status` | KMS v2 gRPC method invocations and outcomes (`status`, `encrypt`, `decrypt`). |
| `openbao_kms_grpc_duration_seconds` | histogram | `method` | Per-method latency for KMS v2 gRPC handlers. |
| `openbao_kms_grpc_in_flight` | gauge | `method` | Current active Status, Encrypt, and Decrypt handler counts. |
| `openbao_kms_grpc_concurrency_rejections_total` | counter | `method` | KMS v2 requests rejected because the method reached its configured active-request limit. |

### OpenBao Calls

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_openbao_requests_total` | counter | `operation`, `status` | OpenBao API call counts by operation (`jwt_login`, `cert_login`, `token_renew_self`, `transit_metadata_read`, `transit_disable_upsert_read`, `transit_encrypt`, `transit_decrypt`, `transit_batch_decrypt`, `capabilities_self`) and outcome. |
| `openbao_kms_openbao_duration_seconds` | histogram | `operation` | Per-operation latency for OpenBao calls. |

Metric `operation` label values are normalized for Prometheus. The matching log `openbao_operation` field uses the unnormalized operation names with spaces.

### Auth And Token

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_auth_login_total` | counter | `status` | Auth-method login attempts by outcome. |
| `openbao_kms_auth_renewal_total` | counter | `status` | Token renewal attempts by outcome. |
| `openbao_kms_auth_method_info` | gauge | `method` | Reports `1` for the configured bounded auth method (`jwt`, `cert`, or `unknown`). |
| `openbao_kms_certificate_source_info` | gauge | `source` | Reports `1` for the configured bounded certificate source (`pkcs11`, `spiffe`, `none`, or `unknown`). |
| `openbao_kms_token_ttl_seconds` | gauge | none | Remaining TTL of the current OpenBao token. |
| `openbao_kms_certificate_ttl_seconds` | gauge | none | Remaining TTL of the current cert-auth client certificate. Zero when certificate auth is not in use or no certificate has been observed. |

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
| `openbao_kms_rotation_state` | gauge | `state` | Reports `1` for the current bounded rotation state (`active`, `pending`, `unknown`). Use `rotation-plan` for detailed promotion state. |

### Validation Errors

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_aad_validation_errors_total` | counter | `reason` | Additional authenticated data (AAD) validation failures during decrypt. |
| `openbao_kms_decrypt_key_id_errors_total` | counter | `reason` | Decrypt rejections caused by unknown, malformed, or stale-disallowed `key_id`. |

### Runtime Health

| Metric | Type | Labels | Description |
|---|---|---|---|
| `openbao_kms_circuit_breaker_state` | gauge | none | OpenBao client circuit breaker state. |
| `openbao_kms_panic_recoveries_total` | counter | `method` | Recovered KMS handler panics by bounded method. Panic values are not exported. |
| `openbao_kms_socket_restarts_total` | counter | none | Socket reclamations after stale socket detection. |

## Log Fields

Stable JSON log fields. Operators can rely on these names across preview patch releases.

| Field | Type | Description |
|---|---|---|
| `ts` | string | RFC 3339 timestamp. |
| `level` | string | Log level (`debug`, `info`, `warn`, `error`). |
| `message` | string | Stable event name (`kms.request`, `openbao.request`, `auth.login`, `auth.renewal`, `status.probe`, `socket.stale_removed`). |
| `operation` | string | Logical operation name in logs (`kms.encrypt`, `kms.decrypt`, `kms.status`, `openbao.request`, `auth.login`, `auth.renewal`, `status.probe`, `socket.stale_removed`). |
| `openbao_operation` | string | Specific OpenBao call when `operation=openbao.request` (`jwt login`, `cert login`, `token renew self`, `transit metadata read`, `transit disable_upsert read`, `transit encrypt`, `transit decrypt`, `transit batch decrypt`, `capabilities self`). |
| `status` | string | Operation outcome (`ok`, `error`). |
| `duration_ms` | number | Operation latency in milliseconds. |
| `key_id_hash` | string | `base64url-sha256` of the active `key_id`. Never the raw `key_id`. |
| `transit_key_version` | number | OpenBao Transit key version associated with the operation. |
| `openbao_request_id` | string | OpenBao request ID when debug correlation is enabled and OpenBao returned a safe ID. |
| `probe_kind` | string | Status-controller probe kind (`metadata`, `deep`) on `status.probe` events. |
| `healthz` | string | KMS v2 Status health value on `kms.status` request events. |
| `error_class` | string | One of the stable error classes; see [Observability: Error Classes](/reference/observability/#error-classes). |
| `request_uid_hash` | string | Hash of the KMS request UID when debug correlation is enabled. |
| `debug_correlation_incident` | string | Operator-supplied incident ID when debug correlation is enabled. |
| `debug_correlation_expires_at` | string | RFC 3339 timestamp at which debug correlation will expire. |
| `panic_recovered` | boolean | Present only when a KMS handler panic was recovered and redacted. |
| `panic_type` | string | Runtime type of the recovered panic value. The panic value itself is never logged. |

The provider must never include `plaintext`, `jwt`, `openbao_token`, `ciphertext`, `transit_key_material`, raw `openbao_path`, or raw `key_name` fields in any log entry.
