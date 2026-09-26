---
title: Performance evidence
description: "Captured kubeadm cold-start and warmup results, what they show about decrypt load, and why the provider keeps the direct decrypt path."
eyebrow: Contribute
weight: 57
---

These results come from controlled validation environments and support release
decisions. They are not performance guarantees, SLOs, or capacity claims.

## Conclusion

The runs show no need for decrypt micro-batching in the current release line.
Growing the Secret corpus from 10,000 to 50,000 objects raised API server list
latency, but provider and OpenBao decrypt counts stayed flat and error-free.
The bottleneck was Kubernetes object creation and large list handling, not
provider decrypt fan-out. A batching coalescer would add queueing,
cancellation, fairness, and per-item error handling without a demonstrated
need; if a workload ever shows decrypt fan-out as the limit, compare batched and
direct paths in a new benchmark.

The release gate also runs the decrypt and load soak lanes described in
[E2E framework](/contribute/e2e-framework/), which catch regressions in
cancellation, latency, and resource growth for the pinned CI environment.

## Environment

| Field | Value |
|---|---|
| Kubernetes | `1.34.3` |
| OpenBao | `2.5.3`, single external instance |
| Topology | Local virtualized kubeadm cluster with three control-plane nodes |
| Provider | Validation build as a static pod per control-plane node, KMS v2 over node-local sockets |

These runs do not cover OpenBao HA failover under cold-start load.

## Results

| Run | Date | Secret corpus | API endpoints | Object reads | Errors | p95 | Max | Provider decrypts | Transit decrypts | Result |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| Cold start | 2026-05-11 | 10,000 | 3 | 30,000 | 0 | 2.497s | 2.497s | 21 | 21 | Passed |
| Cold start | 2026-05-11 | 50,000 | 3 | 150,000 | 0 | 19.78s | 19.78s | 21 | 24 | Passed |
| Sustained warmup | 2026-05-11 | 10,000 | 3 workers | 21,050,000 | 2 | 2.543s | 48.822s | Not proportional to reads | | Informational |

The cold-start runs prepare an encrypted Secret corpus, check representative
raw etcd envelopes, restart every API server in parallel, list the full corpus
once through each, and compare provider and OpenBao decrypt counters before
and after the restart window. With one list per endpoint, p95 can equal max.
"Object reads" counts Secrets returned by list calls, not KMS Decrypt calls.

The warmup run listed the corpus in a loop for 30 minutes. It recorded two
client-side Kubernetes transport errors while the cluster, providers, and
OpenBao stayed healthy, so it is informational; the cold-start runs are the
cleaner evidence.

The 50,000 Secret run exposed slow serial seeding and large deletes in the VM
environment, so the harness now supports corpus reuse, non-blocking deletes,
progress output, and configurable seed workers. The harness and its commands
are maintainer tools kept next to their implementation in the repository.
