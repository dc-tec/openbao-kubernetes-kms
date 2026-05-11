# 0009: Reserve Decrypt Micro-Batching Behind Config

## Status

Accepted for design, revised during implementation.

## Context

API server startup can create a decrypt storm. OpenBao Transit supports batch operations, but batching adds queueing, cancellation, per-item error mapping, AAD handling, and head-of-line blocking risk.

## Decision

Reserve decrypt micro-batching configuration in v0.1, but reject enablement until sustained direct decrypt soak plus local-only Harvester kubeadm decrypt-warmup and cold-start evidence shows a release-blocking need for a production-grade KMS coalescer. The OpenBao Transit client may support `batch_input`, but the KMS request coalescer is outside the v0.1 release boundary unless release-gate evidence fails the direct decrypt path.

Default config:

```yaml
performance:
  decryptMicroBatching:
    enabled: false
    maxBatchSize: 32
    maxWait: 2ms
```

## Consequences

- The implementation needs batch queue tests.
- Release gates must capture sustained direct decrypt soak and local-only Harvester kubeadm cold-start evidence before deciding whether the production coalescer is needed for v0.1.
- If implemented later, benchmarks must compare batched and non-batched startup decrypt storm behavior.
- Per-item errors must preserve KMS semantics.
- The default remains the simpler direct decrypt path.
