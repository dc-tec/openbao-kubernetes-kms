---
title: "First Encrypt"
description: "Verify end-to-end encryption: create a probe Secret, confirm storage in etcd is ciphertext, and read the provider's health and metric signals."
weight: 50
---

# First Encrypt

Run this end-to-end validation after the provider is running, [Kubernetes
Encryption Config](/getting-started/kubernetes-encryption-config/) is complete,
and the API server has reloaded or restarted. The checks confirm that the API
server encrypts selected resources through the provider, stores ciphertext in
etcd, and receives healthy provider signals.

## Step 1: Create A Probe Secret

Create a Secret with a value that is unique enough to identify in etcd output:

```sh
kubectl create secret generic openbao-kms-first-encrypt \
  --from-literal=value='probe-do-not-store-plaintext'
```

Read it back through kubectl:

```sh
kubectl get secret openbao-kms-first-encrypt \
  -o jsonpath='{.data.value}' | base64 -d
```

Expected output: `probe-do-not-store-plaintext`. If the read fails, check the API server log for the encryption provider error class (see [Reference: Observability](/reference/observability/) for the catalog).

## Step 2: Confirm The Stored Value Is Encrypted

This step requires direct access to etcd on a control-plane node and the public
key infrastructure (PKI) material that the API server uses to connect to etcd.
Run it only from a controlled administrative environment.

```sh
ETCDCTL_API=3 etcdctl \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/apiserver-etcd-client.crt \
  --key=/etc/kubernetes/pki/apiserver-etcd-client.key \
  get /registry/secrets/default/openbao-kms-first-encrypt \
  -w fields | head
```

The value field begins with the KMS v2 envelope prefix:

```text
k8s:enc:kms:v2:openbao-kms-workload-a:
```

The provider name in the prefix must match the `name` field of the `EncryptionConfiguration`. The bytes following the prefix are ciphertext. The probe string `probe-do-not-store-plaintext` must not appear anywhere in the output.

Do not store etcd output in logs or untrusted shells. Do not run this inspection from a developer workstation against production etcd.

## Step 3: Confirm The Provider Signals Are Healthy

The provider exposes health endpoints and Prometheus metrics on the addresses
configured in `config.yaml`. The commands below use the default addresses:
`server.healthAddress` on `127.0.0.1:8082` and `server.metricsAddress` on
`127.0.0.1:8081`.

Check liveness and readiness:

```sh
curl -sf http://127.0.0.1:8082/live
curl -sf http://127.0.0.1:8082/ready
```

Both commands exit with status `0`, which confirms that both endpoints return
HTTP 200. `/ready` reports OpenBao reachability, auth validity, Transit metadata
freshness, active key snapshot availability, and cached KMS Status freshness.
A non-200 response on `/ready` can precede read failures because the API server
can still serve cached data.

Confirm key_id stability through the metric:

```sh
curl -sf http://127.0.0.1:8081/metrics | grep openbao_kms_status_key_id_hash
```

Expected output: one line with a single hash value, for example

```text
openbao_kms_status_key_id_hash{hash="uK..."} 1
```

The same hash must be reported by every control-plane node. Different hashes across nodes indicate split-brain on the active key snapshot and require investigation before relying on the encryption layer.

Confirm encrypt and decrypt are landing on the provider:

```sh
curl -sf http://127.0.0.1:8081/metrics | grep -E 'openbao_kms_grpc_requests_total\{method="(encrypt|decrypt)"'
```

The encrypt counter increases when the probe Secret is written. The decrypt
counter might not increase on every read because the Kubernetes API server can
serve some reads from cache. An API server restart or cold-cache read causes
decrypt traffic to reach the provider.

For the full metric and log catalog see [Reference: Observability](/reference/observability/) and [Reference: Metrics](/reference/metrics/).

## Step 4: Clean Up

Delete the probe Secret:

```sh
kubectl delete secret openbao-kms-first-encrypt
```

## Validation Checklist

After this page:

- A Kubernetes Secret round-trips through kubectl with the expected plaintext.
- The same Secret is stored in etcd as a `k8s:enc:kms:v2:` envelope, with no plaintext leaking.
- The provider reports a single stable key_id hash on every control-plane node.
- The provider's `/ready` endpoint returns HTTP 200.
- The provider's encrypt counter increments on the probe write, and decrypt counters remain available for cold-cache or API server restart validation.

## Read Next

1. [Operations: Rotation](/operations/rotation/) once the encryption layer is in steady state.
2. [Operations: Disaster Recovery](/operations/disaster-recovery/) to plan recovery posture before relying on the provider in production.
3. [Reference: Key ID And AAD](/reference/key-id-and-aad/) for the full `key_id` format and additional authenticated data (AAD) envelope.
