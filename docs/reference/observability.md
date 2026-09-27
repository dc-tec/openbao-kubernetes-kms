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

The response is a cached view; requesting `/ready` does not contact OpenBao.
An unhealthy response includes `reasons`, a list of bounded condition codes.
`metadata_error_class` and `deep_error_class`, when present, identify the last
failed or skipped probe. Successful probes clear their own failure details.
A healthy response has an empty `reasons` list. For example:

```json
{"status":"unavailable","healthz":"unhealthy","cache_age_ms":120,"stale":false,"rotation_state":"active","reasons":["metadata_read_failed"],"metadata_error_class":"tls_failed"}
```

The optional `persistence_degraded: true` field reports a deferred pending
observation save. Readiness can remain HTTP 200 only while the published keys
remain validated and both persistence files are confirmed unchanged. The field
does not override other health failures and clears after successful state
publication. See [Persistence failures](/docs/architecture/rotation-model/#persistence-failures).

| Reason | Condition or action |
|---|---|
| `state_unavailable` | No active registry state is loaded, or metadata cannot safely rebuild missing state. |
| `status_stale` | The cached metadata observation is absent or older than `status.statusMaxStaleness`. |
| `metadata_unverified` | Loaded state has not passed a metadata probe. |
| `upsert_check_failed`, `metadata_read_failed` | An OpenBao read failed. Use the probe error class to identify transport, auth, or API failures. |
| `upsert_allowed` | Set Transit `disable_upsert` to `true`. |
| `profile_invalid`, `version_rollback` | Restore the required Transit profile or investigate a metadata rollback. |
| `state_save_failed`, `state_publish_failed` | Inspect state storage permissions, capacity, and registry validity. |
| `encryption_blocked` | The active version is below the encryption minimum. Promotion must complete before writes resume. |
| `deep_probe_pending` | The active key has not passed its required encrypt/decrypt probe. |
| `deep_probe_failed`, `deep_probe_invalid` | The round trip failed or returned an invalid version or oversized ciphertext. |
| `circuit_breaker_open` | A probe was skipped during dependency backoff. |
| `probe_failed` | A probe failed without a more specific condition code. |
| `diagnostics_unavailable` | The readiness handler could not read local diagnostics. |

## Metrics

OAuth token acquisition failures use bounded `status` values on
`openbao_kms_auth_login_total`: `oauth2_credential` for local credential failures,
`oauth2_request` for transport failures, `oauth2_rejected` for non-200 responses,
and `oauth2_response` for invalid responses. JWT claim failures retain the
existing JWT status classes. Remote response bodies and credentials are never
included in these labels or error messages.

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
| `openbao_kms_rotation_persistence_degraded` | gauge | none | `1` while an observation-only save is deferred; `0` after successful state publication. Readiness reports current key usability. |
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
| `message` | Event name: `kms.request`, `openbao.request`, `auth.login`, `auth.renewal`, `status.probe`, `key.promoted`, `serve.start`, `serve.ready`, `serve.shutdown`, or `socket.stale_removed`. |
| `operation` | `kms.encrypt`, `kms.decrypt`, `kms.status`, or the event name. |
| `openbao_operation` | The OpenBao call for `openbao.request` events. |
| `status`, `duration_ms` | Outcome (`ok`, `error`) and latency. |
| `key_id_hash`, `transit_key_version` | Hash of the active `key_id` and the Transit version used. |
| `error_class` | One of the [error classes](#error-classes). |
| `reason` | Bounded failure condition on `status.probe`; uses the readiness codes above. |
| `previous_key_id_hash`, `previous_transit_key_version` | Previous active key on `key.promoted`. The current key uses `key_id_hash` and `transit_key_version`. |
| `probe_kind`, `healthz` | Probe kind (`metadata`, `deep`) and KMS Status health value. |
| `panic_recovered`, `panic_type` | Present after a recovered panic; the panic value is never logged. |
| `openbao_request_id`, `request_uid_hash`, `debug_correlation_incident`, `debug_correlation_expires_at` | Present only during [debug correlation](#debug-correlation). |

Logs never contain plaintext, JWTs, OpenBao tokens, full ciphertext, key
material, full annotation maps, or, by default, raw OpenBao paths and key
names.

At the default `info` level, `serve.start` marks the start of runtime setup.
`serve.ready` follows successful bootstrap probes and listener startup. It is a
startup event; use `/ready` or KMS Status for ongoing health. `serve.shutdown`
follows startup failure or runtime exit. Failures use `startup_failed` or
`runtime_failed` without raw error details. Invalid configuration or logging
settings can fail before structured logging starts.

`key.promoted` logs once when a previously active key changes, after state is
saved and published. Bootstrap, pending observations, and failed state saves do
not emit promotion events. The new key still needs its deep probe before
Encrypt becomes available. Probe failures log at warning level with `reason`
and `error_class`; successful periodic probes remain at debug level.

A deferred observation save logs `reason: state_save_failed` and increments the
failed metadata-probe counter even when cached KMS Status remains healthy.

## Error classes

Failed KMS calls carry a bounded `error_class`. Use these classes as alert
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

Status probe logs and readiness probe error fields use the OpenBao classes
without a prefix: `invalid_request`, `unauthenticated`, `permission_denied`,
`not_found`, `decrypt_failed`, `rate_limited`, `unavailable`, `sealed`,
`tls_failed`, `dns_failed`, `connection_failed`, and `unknown`. Rejected logins
and local credential failures use `auth_failed`; cancellation and deadlines use
`canceled` and `timeout`. Local state or validation failures use their readiness
reason code as the error class. Raw OpenBao responses and local filesystem
errors are never copied into probe log fields or readiness responses.

## Alerts

Alert on: KMS Status unhealthy, stale Status cache, OpenBao error rate, login or
renewal failures, low token TTL, different `key_id` hashes across nodes, a
rotation stuck pending, AAD or unknown `key_id` errors, encrypt or decrypt
latency, rising concurrency rejections, restart loops, and stale socket
reclaims.

Alert when `openbao_kms_rotation_persistence_degraded == 1` persists across
probes. Repair state storage before the next key transition; healthy readiness
during a deferred observation save does not mean rotation can continue.

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

## Clock corrections and cached health

Status cache age is the greatest age observed from elapsed time or wall time.
A backward clock correction cannot make the cache younger. Once cached status
is stale, only a fresh successful metadata probe can restore metadata health;
the existing deep-probe requirements still apply. A forward correction or host
suspension can expire the cache early. Status continues to use cached evidence
and does not contact OpenBao.

Retry, discovery, and circuit-breaker cooldowns use elapsed time. UTC deadline
fields are projections onto the current wall clock. Token TTL and cache-age
metrics report the conservative duration used by the corresponding validity check.
