---
title: "Observability Deployment"
description: "Deploy the Prometheus scrape, alert rules, and Grafana dashboard samples for bao-kms-provider."
weight: 50
---

# Observability Deployment

`bao-kms-provider` exposes Prometheus metrics on `server.metricsAddress` at
`/metrics`. The default listen address is `127.0.0.1:8081`, so a production
scrape normally needs a node-local Prometheus agent, host networking, or another
explicit local forwarding model. Do not expose the metrics endpoint on a
routable interface unless the surrounding control-plane monitoring design
requires it.

## Prometheus

Scrape the provider on every control-plane node. Keep the scrape labels stable
enough to distinguish nodes and compare active key state across the fleet.

Minimum scrape target:

```yaml
scrape_configs:
  - job_name: openbao-kubernetes-kms
    static_configs:
      - targets:
          - 127.0.0.1:8081
```

Example alerting rules live at
`deploy/prometheus/rules/openbao-kms.rules.yaml`. Treat
them as starting points; tune thresholds to the configured probe cadence, OpenBao
latency, token TTLs, and API server restart behavior before using them for
paging.

## Grafana

The maintained dashboard sample lives at:

```text
deploy/grafana/dashboards/openbao-kms-overview.json
```

Import it into Grafana with a Prometheus data source whose UID is `Prometheus`,
or adjust the dashboard data source UID during import. The dashboard covers:

- KMS gRPC request rate, error ratio, and p95/p99 latency,
- OpenBao request rate, error ratio, and p95/p99 latency,
- status cache age, token TTL, certificate TTL, and circuit breaker state,
- active Transit key version, active `key_id` hash convergence, and rotation state,
- auth failures, Transit metadata probe failures, decrypt validation errors,
- panic recovery and stale socket cleanup counters.

For the metric contract, see [Reference: Metrics](/reference/metrics/). For
health and alerting semantics, see [Reference: Observability](/reference/observability/).
