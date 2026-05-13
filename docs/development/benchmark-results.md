---
title: "Performance Evidence"
description: "Captured load, decrypt warmup, and kubeadm cold-start evidence used for release decisions."
weight: 57
---

# Performance Evidence

This page records benchmark and validation evidence used for release decisions.
The results are release evidence from controlled validation environments, not
general performance guarantees, service-level objectives, or capacity claims.
Use them to understand the observed behavior, the micro-batching decision, and
the remaining validation work before production claims.

## Current Conclusion

The captured runs do not show a release-blocking need for decrypt
micro-batching in the current release line. Direct provider decrypt paths are
covered by E2E soak tests in the repository, and the local kubeadm VM
cold-start runs show that increasing the
Kubernetes Secret corpus from 10,000 to 50,000 objects increased API server
list latency while provider and OpenBao decrypt counter deltas stayed small.

The bottleneck observed in the 50,000 Secret validation run was Kubernetes object
creation and large Secret list handling. It was not provider decrypt fan-out.

## Validation Environment

| Field | Value |
|---|---|
| Provider | `bao-kms-provider` validation build |
| Kubernetes | `1.34.3` |
| OpenBao | `2.5.3` |
| Topology | Local virtualized kubeadm environment with three control-plane nodes |
| Provider deployment | Static pod per control-plane node |
| OpenBao deployment | Single external OpenBao instance for the captured runs |
| KMS mode | Kubernetes KMS v2 over node-local Unix sockets |

These runs do not validate OpenBao HA failover behavior under cold-start load.

## Summary

### Automated Release Soak

The release gate runs two local OpenBao soak lanes from
`test/e2e/suites.yaml`:

- `test-e2e-provider-decrypt-soak-openbao-ci` exercises sustained direct
  Decrypt requests through the provider and OpenBao Transit.
- `test-e2e-provider-load-soak-openbao-ci` exercises sustained Status, Encrypt,
  and Decrypt requests through the provider and OpenBao Transit.

These lanes are automated release evidence for the pinned CI environment only.
They are useful for catching regressions in request cancellation, latency, and
resource growth, but they do not establish production throughput, capacity, or
availability guarantees.

### Kubeadm VM Runs

| Run | Date | Secret corpus | API endpoints | Object reads | Errors | p95 | Max | Provider decrypt delta | Transit decrypt delta | Result |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| Kubeadm VM cold start | 2026-05-11 | 10,000 | 3 | 30,000 | 0 | 2.497s | 2.497s | 21 | 21 | Passed |
| Kubeadm VM cold start | 2026-05-11 | 50,000 | 3 | 150,000 | 0 | 19.78s | 19.78s | 21 | 24 | Passed |
| Kubeadm VM sustained warmup | 2026-05-11 | 10,000 | 3 workers | 21,050,000 | 2 | 2.543s | 48.822s | See notes | See notes | Informational |

The sustained warmup run is informational because it recorded two client-side
Kubernetes transport errors during a 30 minute list loop. The cluster,
providers, and OpenBao remained healthy after the run. The cold-start runs are
the cleaner evidence for API server restart behavior because they capture
provider counters before and after the restart window.

`Object reads` means Kubernetes Secret objects returned by API server list
responses. It is not the number of KMS Decrypt RPCs observed by the provider.
For cold-start runs, each API endpoint performs one full-corpus list operation,
so the percentile sample set is intentionally small and p95 may equal max.
Provider decrypt deltas were calculated from provider Prometheus metrics before
and after the restart window. Transit decrypt deltas were calculated from
OpenBao Transit metrics over the same window.

## Kubeadm VM Cold Start

The cold-start validation prepares or reuses an encrypted Secret corpus,
verifies representative raw etcd envelopes, captures provider metrics, restarts
all selected kube-apiserver instances, lists the full corpus once through each
API server, and captures provider metrics again.

### 10,000 Secret Run

This run read 30,000 Secret objects across three API server endpoints after
parallel kube-apiserver restart. The provider recorded 21 successful decrypt
RPCs and zero decrypt errors.

### 50,000 Secret Run

This run read 150,000 Secret objects across three API server endpoints after
parallel kube-apiserver restart. The provider recorded the same successful
decrypt RPC delta as the 10,000 Secret run and zero decrypt errors. OpenBao
Transit decrypt delta remained small at 24 requests.

The 50,000 Secret run exposed setup cost in the VM validation environment:
serial seeding and large Kubernetes deletes were slow enough that the helper
now supports corpus reuse, non-blocking deletes with count polling, chunk
progress output, and configurable seed workers.

## Kubeadm VM Sustained Warmup

The sustained warmup validation repeatedly lists the encrypted Secret corpus
for a fixed duration. It is useful for exercising Kubernetes object read
behavior over time, but Kubernetes client transport errors make it less precise
as release evidence than the bounded cold-start validation.

After the run, the multi-control-plane API servers, provider static pods, and
OpenBao service were healthy. Provider decrypt counters were not proportional to
the `secret_objects_read` value, which is consistent with the cold-start
results.

## Micro-Batching Decision

Decrypt micro-batching is not implemented in the provider runtime.
The evidence so far points to these conclusions:

- Kubernetes object list cost grows with corpus size.
- Provider decrypt RPC count did not grow with corpus size in the cold-start
  runs.
- OpenBao Transit decrypt errors stayed at zero.
- A production KMS coalescer would add queueing, cancellation, fairness, and
  per-item error handling complexity without a demonstrated release need.

Future benchmark work should compare batched and direct decrypt paths only if a
new workload shows provider or OpenBao decrypt fan-out as the limiting factor.

## Reproduction

The local VM harness and infrastructure-specific commands are maintainer tools
and are intentionally kept outside the public documentation site. They live
next to the harness implementation in the repository.
