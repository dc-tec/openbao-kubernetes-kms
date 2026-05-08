# Observability

This document defines logs, metrics, and health endpoints.

## Principles

- KMS `Status` is the health signal consumed by kube-apiserver.
- HTTP health endpoints are for local operations and monitoring.
- Metrics must avoid secrets and high-cardinality labels.
- Logs must be structured and redacted.
- Key IDs may be public, but metrics and logs should prefer hashes.

## Metrics

Recommended Prometheus metrics:

```text
openbao_kms_grpc_requests_total{method,status}
openbao_kms_grpc_duration_seconds{method}
openbao_kms_openbao_requests_total{operation,status}
openbao_kms_openbao_duration_seconds{operation}
openbao_kms_status_key_id_hash{hash}
openbao_kms_key_version
openbao_kms_token_ttl_seconds
openbao_kms_auth_renewal_total{status}
openbao_kms_auth_login_total{status}
openbao_kms_circuit_breaker_state
openbao_kms_decrypt_batch_size
openbao_kms_socket_restarts_total
openbao_kms_status_cache_age_seconds
openbao_kms_transit_metadata_observation_total{status}
openbao_kms_rotation_state{state}
openbao_kms_aad_validation_errors_total{reason}
openbao_kms_decrypt_key_id_errors_total{reason}
```

Label rules:

- Use bounded values.
- Hash key IDs.
- Do not expose raw OpenBao paths.
- Do not expose raw key names.
- Do not label with Kubernetes object names.
- Do not label with request UIDs.
- Do not label with full error strings.

## Logs

Use JSON logs.

Example:

```json
{
  "ts": "2026-05-08T12:00:00Z",
  "level": "info",
  "operation": "kms.decrypt",
  "status": "ok",
  "duration_ms": 4.2,
  "key_id_hash": "uK...",
  "transit_key_version": 3,
  "openbao_request_duration_ms": 3.1,
  "openbao_request_id": "optional-redacted",
  "error_class": ""
}
```

Never log:

- plaintext,
- JWT,
- OpenBao token,
- full ciphertext,
- raw Transit key material,
- raw OpenBao paths by default,
- raw key names by default,
- full annotation maps.

## Error Classes

Use stable error classes:

- `config_invalid`
- `socket_unavailable`
- `auth_failed`
- `auth_expired`
- `openbao_unavailable`
- `transit_key_missing`
- `transit_policy_denied`
- `key_id_unknown`
- `key_id_malformed`
- `aad_missing`
- `aad_mismatch`
- `annotation_invalid`
- `status_stale`
- `timeout`

## Health Endpoints

Recommended endpoints:

```text
/live
/ready
/metrics
```

`/live` should report:

- process is running,
- gRPC server initialized,
- socket listener initialized.

`/ready` should report:

- OpenBao reachable,
- auth valid,
- Transit metadata fresh,
- active key snapshot available,
- cached KMS Status fresh.

`/metrics` should expose Prometheus metrics.

Bind health and metrics to localhost by default.

## Alerts

Recommended alerts:

- KMS Status unhealthy.
- Status cache age exceeds threshold.
- OpenBao request error rate above threshold.
- Auth login/renewal failures.
- Token TTL below threshold.
- Key ID hash differs across control-plane nodes.
- Rotation state stuck pending.
- AAD validation errors.
- Unknown key ID errors.
- Latency SLO burn for encrypt/decrypt.
- Plugin restart loop.
- Socket restart or stale socket detection.

## Correlation With OpenBao

OpenBao request IDs may be logged when available and safe. They should not be stored in KMS annotations by default.

Debug correlation mode should be:

- disabled by default,
- time-limited,
- documented in incident notes,
- verified not to log secrets.

