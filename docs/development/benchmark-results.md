---
title: "Benchmark Results"
description: "Captured performance and recovery evidence for provider load, decrypt warmup, and kubeadm cold-start behavior."
weight: 57
---

# Benchmark Results

This page records benchmark and lab evidence used for v0.1 release decisions.
The results are local release evidence, not general performance guarantees. Use
them to understand observed behavior, the micro-batching decision, and the
remaining validation work before production claims.

## Current Conclusion

The captured runs do not show a release-blocking need for decrypt
micro-batching in v0.1. Direct provider decrypt paths are covered by CI soak
tests, and the Harvester kubeadm cold-start runs show that increasing the
Kubernetes Secret corpus from 10,000 to 50,000 objects increased API server list
latency, while provider and OpenBao decrypt counter deltas stayed small.

The bottleneck observed in the 50,000 Secret local lab run was Kubernetes object
creation and large Secret list handling. It was not provider decrypt fan-out.

## Environment

| Field | Value |
|---|---|
| Provider | `bao-kms-provider` engineering-preview build |
| Kubernetes | `1.34.3` |
| OpenBao | `2.5.3` |
| Lab topology | Harvester VMs, kubeadm, three control-plane nodes |
| Provider deployment | Static pod per control-plane node |
| OpenBao deployment | Single OpenBao VM for the captured Harvester runs |
| KMS mode | Kubernetes KMS v2 over node-local Unix sockets |

## Summary

| Run | Date | Secret corpus | API endpoints | Object reads | Errors | p95 | Max | Provider decrypt delta | Transit decrypt delta | Result |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| Harvester cold start | 2026-05-11 | 10,000 | 3 | 30,000 | 0 | 2.497s | 2.497s | 21 | 21 | Passed |
| Harvester cold start | 2026-05-11 | 50,000 | 3 | 150,000 | 0 | 19.78s | 19.78s | 21 | 24 | Passed |
| Harvester sustained warmup | 2026-05-11 | 10,000 | 3 workers | 21,050,000 | 2 | 2.543s | 48.822s | See notes | See notes | Informational |

The sustained warmup run is listed as informational because it recorded two
client-side Kubernetes transport errors during a 30 minute list loop. The
cluster, providers, and OpenBao remained healthy after the run. The cold-start
runs are the cleaner evidence for API server restart behavior because they
capture provider counters before and after the restart window.

## Harvester Cold Start

The cold-start command prepares or reuses an encrypted Secret corpus, verifies
representative raw etcd envelopes, captures provider metrics, restarts all
selected kube-apiserver instances, lists the full corpus once through each API
server, and captures provider metrics again.

### 10,000 Secret Run

```text
harvester_decrypt_cold_start cluster=mcp secrets=10000 endpoints=3 lists=3 secret_objects_read=30000 errors=0 list_p95=2.497s list_max=2.497s provider_decrypt_delta=21 provider_decrypt_errors_delta=0 transit_decrypt_delta=21 transit_decrypt_errors_delta=0
```

This run read 30,000 Secret objects across three API server endpoints after
parallel kube-apiserver restart. The provider recorded 21 successful decrypt
RPCs and zero decrypt errors.

### 50,000 Secret Run

```text
harvester_decrypt_cold_start cluster=mcp secrets=50000 endpoints=3 lists=3 secret_objects_read=150000 errors=0 list_p95=19.78s list_max=19.78s provider_decrypt_delta=21 provider_decrypt_errors_delta=0 transit_decrypt_delta=24 transit_decrypt_errors_delta=0
```

This run read 150,000 Secret objects across three API server endpoints after
parallel kube-apiserver restart. The provider recorded the same successful
decrypt RPC delta as the 10,000 Secret run and zero decrypt errors.

The 50,000 Secret run exposed setup cost in the local lab. Serial seeding and
large Kubernetes deletes were slow enough that the lab helper now supports
corpus reuse, non-blocking deletes with count polling, chunk progress output,
and configurable seed workers.

## Harvester Sustained Warmup

The sustained warmup command repeatedly lists the encrypted Secret corpus for a
fixed duration. It is useful for exercising Kubernetes object read behavior over
time, but Kubernetes client transport errors make it less precise as release
evidence than the bounded cold-start command.

Captured run:

```text
harvester_decrypt_warmup cluster=mcp secrets=10000 workers=3 duration=30m0s lists=2105 secret_objects_read=21050000 errors=2 list_p95=2.543s list_max=48.822s
```

After the run, the multi-control-plane API servers, provider static pods, and
OpenBao service were healthy. Provider decrypt counters were not proportional to
the `secret_objects_read` value, which is consistent with the cold-start
results.

## Micro-Batching Decision

Decrypt micro-batching remains disabled and rejected by configuration for v0.1.
The evidence so far points to these conclusions:

- Kubernetes object list cost grows with corpus size.
- Provider decrypt RPC count did not grow with corpus size in the cold-start
  runs.
- OpenBao Transit decrypt errors stayed at zero.
- A production KMS coalescer would add queueing, cancellation, fairness, and
  per-item error handling complexity without a demonstrated v0.1 release need.

Future benchmark work should compare batched and direct decrypt paths only if a
new workload shows provider or OpenBao decrypt fan-out as the limiting factor.

## Reproduction Commands

Rerun the 50,000 Secret cold-start check against the existing corpus:

```sh
HARVESTER_INSECURE_SKIP_TLS_VERIFY=true \
HARVESTER_ENABLE_MULTI_CONTROL_PLANE=true \
HARVESTER_DECRYPT_WARMUP_SECRET_COUNT=50000 \
HARVESTER_DECRYPT_WARMUP_REUSE_CORPUS=true \
HARVESTER_DECRYPT_WARMUP_RESTART_PARALLEL=true \
HARVESTER_DECRYPT_COLD_START_TIMEOUT=10m \
HARVESTER_DECRYPT_COLD_START_MAX_P95=60s \
make harvester-lab-verify-decrypt-cold-start
```

Rebuild a large corpus only when the target size or object shape changes:

```sh
HARVESTER_INSECURE_SKIP_TLS_VERIFY=true \
HARVESTER_ENABLE_MULTI_CONTROL_PLANE=true \
HARVESTER_DECRYPT_WARMUP_SECRET_COUNT=50000 \
HARVESTER_DECRYPT_WARMUP_REUSE_CORPUS=false \
HARVESTER_DECRYPT_WARMUP_SEED_BATCH_SIZE=1000 \
HARVESTER_DECRYPT_WARMUP_SEED_WORKERS=4 \
HARVESTER_DECRYPT_WARMUP_RESTART_PARALLEL=true \
HARVESTER_DECRYPT_COLD_START_TIMEOUT=10m \
HARVESTER_DECRYPT_COLD_START_MAX_P95=60s \
make harvester-lab-verify-decrypt-cold-start
```

For setup details, see [Harvester Kubeadm Lab](/development/harvester-kubeadm-lab/).
