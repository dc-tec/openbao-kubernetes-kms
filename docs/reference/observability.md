---
title: Observability
description: "Health endpoints, every Prometheus metric, stable log fields, error classes, recommended alerts, and incident-only debug correlation."
eyebrow: Reference · Observability
weight: 40
verifiedBy:
  - internal/metrics/collectors.go
  - internal/kmsv2/observability.go
  - internal/health/handler.go
  - internal/logging
  - deploy/prometheus/rules/openbao-kms.rules.yaml
---

KMS v2 Status is the health signal `kube-apiserver` uses. The HTTP endpoints,
metrics, and logs on this page serve node-local operations and monitoring. None
of them carry secrets, and `key_id` values appear only as hashes. For scrape
setup and the dashboard, see [Configure: Monitor the provider](/docs/configure/monitor/).

## Endpoints

| Path | Address | Reports |
|---|---|---|
| `/live` | `server.healthAddress`, default `127.0.0.1:8082` | The process, gRPC server, and socket listener are up. |
| `/ready` | `server.healthAddress` | OpenBao is reachable, auth is valid, Transit metadata is fresh, an active key snapshot exists, the last deep probe succeeded, and cached KMS Status is fresh. |
| `/metrics` | `server.metricsAddress`, default `127.0.0.1:8081` | Prometheus metrics. |

`/ready` can fail while API server reads still succeed from its cache, which
makes it the earliest warning.

## Metrics

Labels hold only bounded values. `key_id` values are exported as
`base64url-sha256` hashes. Raw OpenBao paths, key names, Kubernetes object
names, request UIDs, and error strings never appear as labels.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `openbao_kms_grpc_requests_total` | counter | `method`, `status` | KMS v2 calls (`status`, `encrypt`, `decrypt`) by outcome. |
| `openbao_kms_grpc_duration_seconds` | histogram | `method` | KMS v2 handler latency. |
| `openbao_kms_grpc_in_flight` | gauge | `method` | Active handlers per method. |
| `openbao_kms_grpc_concurrency_rejections_total` | counter | `method` | Calls rejected at the method's concurrency limit. |
| `openbao_kms_openbao_requests_total` | counter | `operation`, `status` | OpenBao calls by operation and outcome. |
| `openbao_kms_openbao_duration_seconds` | histogram | `operation` | OpenBao call latency. |
| `openbao_kms_auth_login_total` | counter | `status` | Login attempts by outcome. |
| `openbao_kms_auth_renewal_total` | counter | `status` | Token renewals by outcome. |
| `openbao_kms_auth_method_info` | gauge | `method` | `1` for the configured method: `jwt`, `cert`, or `unknown`. |
| `openbao_kms_certificate_source_info` | gauge | `source` | `1` for the certificate source: `pkcs11`, `spiffe`, `none`, or `unknown`. |
| `openbao_kms_token_ttl_seconds` | gauge | none | Remaining OpenBao token TTL. |
| `openbao_kms_certificate_ttl_seconds` | gauge | none | Remaining client certificate TTL; `0` without certificate auth. |
| `openbao_kms_status_key_id_hash` | gauge | `hash` | `1` for the active `key_id` hash. Must match on every node. |
| `openbao_kms_key_version` | gauge | none | Transit key version used for new encryptions. |
| `openbao_kms_status_cache_age_seconds` | gauge | none | Age of the cached KMS Status response. |
| `openbao_kms_transit_metadata_observation_total` | counter | `status` | Background Transit metadata probes by outcome. |
| `openbao_kms_rotation_state` | gauge | `state` | `1` for `active`, `pending`, or `unknown`; see `rotation-plan` for detail. |
| `openbao_kms_aad_validation_errors_total` | counter | `reason` | AAD validation failures during decryption. |
| `openbao_kms_decrypt_key_id_errors_total` | counter | `reason` | Decryptions rejected for an unknown, malformed, or disallowed `key_id`. |
| `openbao_kms_circuit_breaker_state` | gauge | none | OpenBao client circuit breaker state. |
| `openbao_kms_panic_recoveries_total` | counter | `method` | Recovered handler panics. |
| `openbao_kms_socket_restarts_total` | counter | none | Socket reclaims after a stale socket was detected. |

The `operation` label takes `jwt_login`, `cert_login`, `token_renew_self`,
`transit_metadata_read`, `transit_disable_upsert_read`, `transit_encrypt`,
`transit_decrypt`, `transit_batch_decrypt`, or `capabilities_self`. Logs use the
same names with spaces instead of underscores.

## Logs

`serve` writes structured JSON (`logging.format: json`). Successful
high-frequency KMS and OpenBao requests log at debug level; failures log as
warnings. These fields are stable across preview patch releases:

| Field | Meaning |
|---|---|
| `ts`, `level` | RFC 3339 timestamp and level (`debug`, `info`, `warn`, `error`). |
| `message` | Event name: `kms.request`, `openbao.request`, `auth.login`, `auth.renewal`, `status.probe`, or `socket.stale_removed`. |
| `operation` | `kms.encrypt`, `kms.decrypt`, `kms.status`, or the event name. |
| `openbao_operation` | The OpenBao call for `openbao.request` events. |
| `status`, `duration_ms` | Outcome (`ok`, `error`) and latency. |
| `key_id_hash`, `transit_key_version` | Hash of the active `key_id` and the Transit version used. |
| `error_class` | One of the [error classes](#error-classes). |
| `probe_kind`, `healthz` | Probe kind (`metadata`, `deep`) and KMS Status health value. |
| `panic_recovered`, `panic_type` | Present after a recovered panic; the panic value is never logged. |
| `openbao_request_id`, `request_uid_hash`, `debug_correlation_incident`, `debug_correlation_expires_at` | Present only during [debug correlation](#debug-correlation). |

Logs never contain plaintext, JWTs, OpenBao tokens, full ciphertext, key
material, full annotation maps, or, by default, raw OpenBao paths and key
names.

## Error classes

Every failed operation carries one stable `error_class`. Use them as alert
routing keys and dashboard groups.

| Area | Classes |
|---|---|
| OpenBao and auth | `openbao_unavailable`, `openbao_tls_failed`, `openbao_dns_failed`, `openbao_connection_failed`, `openbao_sealed`, `openbao_rate_limited`, `auth_failed`, `transit_key_missing`, `transit_policy_denied` |
| Decrypt validation | `key_id_unknown`, `key_id_malformed`, `key_metadata_refresh_failed`, `aad_missing`, `aad_mismatch`, `annotation_invalid` |
| Request handling | `status_stale`, `protocol_limit`, `concurrency_limit`, `timeout`, `canceled`, `panic`, `unknown` |

Token errors keep their cause: local credential failures and rejected logins
are `auth_failed`, while transport, unavailable, sealed, rate-limited, canceled,
and timed-out auth calls keep their cause classes. A Transit `403` that persists after the
single recovery attempt, or while recovery is throttled, is
`transit_policy_denied`, because OpenBao does not always tell a revoked token
from a policy denial. OpenBao request metrics count both the rejected attempt
and the retry.

Typed certificate verification and TLS record failures use `openbao_tls_failed`;
DNS failures use `openbao_dns_failed`; socket connection failures, including
connection refusal and reset, use `openbao_connection_failed`. These KMS errors
still return gRPC `Unavailable`. Unrecognized transport failures remain
`openbao_unavailable`. No destination, certificate, or raw transport error is
logged. OpenBao request logs and their metric `status` label use `tls_failed`,
`dns_failed`, and `connection_failed` without the `openbao_` prefix.

## Alerts

Alert on: KMS Status unhealthy, stale Status cache, OpenBao error rate, login or
renewal failures, low token TTL, different `key_id` hashes across nodes, a
rotation stuck pending, AAD or unknown `key_id` errors, encrypt or decrypt
latency, rising concurrency rejections, restart loops, and stale socket
reclaims.

Starting rules ship in `deploy/prometheus/rules/openbao-kms.rules.yaml`. Tune
their thresholds to your OpenBao latency, probe cadence, token TTLs, and scrape
topology before paging on them.

## Debug correlation

Debug correlation temporarily adds `request_uid_hash`, `openbao_request_id`,
and the incident fields to debug logs so you can match provider logs with
`kube-apiserver` and OpenBao audit records. It is off by default and turns on
only when all of these hold:

- `logging.level: debug` and `logging.logOpenBaoRequestIDs: true`,
- `logging.debugCorrelation.incidentId` is set,
- `logging.debugCorrelation.ttl` is positive and at most one hour.

It switches itself off when the TTL expires, without a restart, and never
relaxes the logging rules above. OpenBao request IDs are never stored in KMS
annotations.

```yaml
logging:
  level: debug
  logOpenBaoRequestIDs: true
  debugCorrelation:
    enabled: true
    ttl: 15m
    incidentId: INC-12345
```
