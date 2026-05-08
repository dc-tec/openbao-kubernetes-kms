# 0009: Include Decrypt Micro-Batching Behind Config

## Status

Accepted for design.

## Context

API server startup can create a decrypt storm. OpenBao Transit supports batch operations, but batching adds queueing, cancellation, per-item error mapping, AAD handling, and head-of-line blocking risk.

## Decision

Implement decrypt micro-batching in v0.1, but keep it disabled by default until benchmarks justify enabling it.

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
- Benchmarks must compare batched and non-batched startup decrypt storm behavior.
- Per-item errors must preserve KMS semantics.
- The default remains the simpler direct decrypt path.

