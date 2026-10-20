---
title: Monitor the provider
description: "Scrape provider metrics on every control-plane node, load the sample alert rules, and import the Grafana dashboard."
eyebrow: Configure · Observability
weight: 20
verifiedBy:
  - deploy/prometheus/rules/openbao-kms.rules.yaml
  - deploy/grafana/dashboards/openbao-kms-overview.json
---

Metrics listen on `127.0.0.1:8081` by default, so scrape them with a
node-local Prometheus agent, host networking, or explicit local forwarding.
Expose the endpoint on a routable interface only if your control-plane
monitoring design requires it. For every metric, see
[Reference: Observability](/docs/reference/observability/#metrics).

## Prometheus

Scrape every control-plane node with labels that tell nodes apart, so you can
compare the active `key_id` hash across them:

```yaml
scrape_configs:
  - job_name: openbao-kubernetes-kms
    static_configs:
      - targets:
          - 127.0.0.1:8081
```

Load the sample rules from `deploy/prometheus/rules/openbao-kms.rules.yaml`,
and tune their thresholds to your probe cadence, OpenBao latency, token TTLs,
and API server restart behavior before paging on them.

The `OpenBaoKMSProcessRestarting` alert detects at least three observed process
restarts in 15 minutes. It matches `process_start_time_seconds` only on targets
that also expose `openbao_kms_build_info`. Keep `job` and `instance` labels
stable across restarts. Restarts between scrapes or replacements that change
these labels can go uncounted. `openbao_kms_socket_restarts_total` counts stale
socket cleanup, not process restarts.

## Grafana

Import `deploy/grafana/dashboards/openbao-kms-overview.json` with a Prometheus
data source whose UID is `Prometheus`, or change the UID during import. The
dashboard shows KMS and OpenBao request rates, errors, and latency, Status
cache age, token and certificate TTLs, the circuit breaker, the active key
version, `key_id` convergence and rotation state, auth and probe failures,
decrypt validation errors, panics, and stale socket cleanup.
