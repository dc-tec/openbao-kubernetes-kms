---
title: "Observability"
description: "Principles, error classes, health endpoints, alerts, log shape, and debug correlation for bao-kms-provider."
weight: 30
---

# Observability

This page defines the observability surface of `bao-kms-provider`: principles, log structure, error classes, health endpoints, alerts, and debug correlation. The exhaustive metric and log-field reference is on a separate page; see [Reference: Metrics](/reference/metrics/).

## Principles

- KMS Status is the API server's primary health signal. HTTP health endpoints exist for node-local operations and monitoring.
- Metrics avoid secrets and high-cardinality labels.
- Logs are structured JSON and redacted by default.
- Key IDs may be public; metrics and logs export hashes rather than raw IDs.

## Logs

The provider emits structured JSON logs. `bao-kms-provider serve` defaults to `logging.format: json`. Successful high-frequency KMS and OpenBao request logs are emitted at debug level; failures are warning-level events.

Example log entry:

```json
{
  "ts": "2026-05-08T12:00:00Z",
  "level": "debug",
  "message": "kms.request",
  "operation": "kms.decrypt",
  "status": "ok",
  "duration_ms": 4.2,
  "key_id_hash": "uK...",
  "transit_key_version": 3,
  "error_class": ""
}
```

The provider must never log:

- plaintext,
- JWTs,
- OpenBao tokens,
- full ciphertext,
- raw Transit key material,
- raw OpenBao paths by default,
- raw key names by default,
- full annotation maps.

For the full set of stable log fields see [Reference: Metrics](/reference/metrics/#log-fields).

## Error Classes

The provider tags every failed operation with one of these stable error classes. Use these as alert routing keys and dashboard groupings.

- `config_invalid`
- `socket_unavailable`
- `auth_failed`
- `auth_expired`
- `openbao_rate_limited`
- `openbao_sealed`
- `openbao_unavailable`
- `panic`
- `transit_key_missing`
- `transit_policy_denied`
- `key_id_unknown`
- `key_id_malformed`
- `aad_missing`
- `aad_mismatch`
- `annotation_invalid`
- `protocol_limit`
- `status_stale`
- `timeout`
- `canceled`
- `unknown`

## Health Endpoints

```text
/live      process alive, gRPC server initialized, socket listener initialized
/ready     OpenBao reachable, auth valid, Transit metadata fresh,
           active key snapshot available, cached KMS Status fresh
/metrics   Prometheus metrics
```

`/live` and `/ready` are served on `server.healthAddress`. `/metrics` is served on `server.metricsAddress`. Both default to `127.0.0.1` so neither is exposed on a routable interface without explicit configuration.

`/ready` is the first signal that something is wrong even when API server reads continue to succeed against the API server cache. KMS v2 Status is the canonical health signal consumed by `kube-apiserver`.

## Alerts

Recommended alert conditions:

- KMS Status unhealthy.
- Status cache age exceeds threshold.
- OpenBao request error rate above threshold.
- Auth login or renewal failures.
- Token TTL below threshold.
- `key_id` hash differs across control-plane nodes.
- Rotation state stuck pending.
- AAD validation errors.
- Unknown `key_id` errors.
- Latency threshold breach for encrypt or decrypt.
- Plugin restart loop.
- Socket restart or stale socket detection.

Example Prometheus alerting rules ship at `docs/operations/prometheus-alerts.yaml` (also published as a static asset on this site). Treat the rules as starting points and tune thresholds to local OpenBao latency, probe cadence, token TTLs, and control-plane scrape topology before using them for paging.

## Correlation With OpenBao

OpenBao request IDs may be logged when available and safe. They must not be stored in KMS annotations by default.

Debug correlation mode is disabled by default. When enabled, it temporarily adds safe correlation fields to debug logs:

- `request_uid_hash` on KMS request logs,
- `openbao_request_id` on OpenBao request logs when OpenBao returned a safe request ID,
- `debug_correlation_incident`,
- `debug_correlation_expires_at`.

The mode has strict guardrails:

- disabled by default,
- requires `logging.level: debug`,
- requires `logging.logOpenBaoRequestIDs: true`,
- requires `logging.debugCorrelation.incidentId`,
- requires a positive `logging.debugCorrelation.ttl` no greater than one hour,
- expires automatically without restart after the configured TTL,
- still does not log plaintext, JWTs, OpenBao tokens, full ciphertext, raw Transit key material, raw OpenBao paths, or raw key names.

Example incident-only configuration:

```yaml
logging:
  level: debug
  logOpenBaoRequestIDs: true
  debugCorrelation:
    enabled: true
    ttl: 15m
    incidentId: INC-12345
```

For the configuration field reference see [Configuration: Debug Correlation](/reference/configuration/#debug-correlation).
