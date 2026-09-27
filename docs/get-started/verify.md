---
title: Verify encryption
description: "Confirm that etcd stores the probe Secret as KMS v2 ciphertext and that every provider reports the same healthy key."
eyebrow: Get started · Step 9
weight: 90
verifiedBy:
  - test/dev-env/scripts/verify-kms.sh
  - internal/metrics
  - internal/health
---

A Secret that reads back through `kubectl` proves only that the API server
works. This step checks that the probe is stored as ciphertext and that every
provider is healthy. It does not prove complete encryption of existing data.
Keep the `identity` reader for plaintext objects in this preview evaluation.

## Step 1: Create a probe Secret

Create a Secret with a value you can search for, and read it back:

```sh
kubectl create secret generic openbao-kms-first-encrypt \
  --from-literal=value='probe-do-not-store-plaintext'
kubectl get secret openbao-kms-first-encrypt -o jsonpath='{.data.value}' | base64 -d
```

The second command prints `probe-do-not-store-plaintext`. If it fails, check
the API server log for the provider error class; see
[Reference: Observability](/docs/reference/observability/).

## Step 2: Check the stored value in etcd

On a control-plane node, read the Secret straight from etcd with the `etcdctl`
inside the etcd static pod, and check the stored bytes without printing them:

```sh
ETCD_CID=$(sudo crictl ps --name etcd -q | head -n1)
etcd_get() {
  sudo crictl exec "${ETCD_CID}" etcdctl \
    --endpoints=https://127.0.0.1:2379 \
    --cacert=/etc/kubernetes/pki/etcd/ca.crt \
    --cert=/etc/kubernetes/pki/etcd/server.crt \
    --key=/etc/kubernetes/pki/etcd/server.key \
    get "$1"
}
etcd_get /registry/secrets/default/openbao-kms-first-encrypt | grep -a -o 'k8s:enc:kms:v2:[^:]*:'
etcd_get /registry/secrets/default/openbao-kms-first-encrypt | grep -a -c 'probe-do-not-store-plaintext'
```

The first check prints `k8s:enc:kms:v2:` followed by your provider name, for
example `k8s:enc:kms:v2:openbao-kms-workload-a:`. The second prints `0`: the
plaintext appears nowhere in the stored value. The commands assume kubeadm's
stacked etcd; for external etcd, run `etcdctl` with that cluster's client
certificates.

## Step 3: Check the provider on every node

On each control-plane node, check the provider's health endpoints and the
active `key_id` hash:

```sh
curl -fsS http://127.0.0.1:8082/live
curl -fsS http://127.0.0.1:8082/ready
curl -fsS http://127.0.0.1:8081/metrics | grep openbao_kms_status_key_id_hash
```

Both health checks return HTTP 200. `/ready` covers OpenBao reachability, auth
validity, Transit metadata freshness, the active key snapshot, and KMS Status
freshness. The metric prints one line, such as
`openbao_kms_status_key_id_hash{hash="uK..."} 1`, and every node must print the
same hash. Different hashes mean the nodes disagree about the active key;
stop and investigate before you continue.

To confirm that encrypt calls reach the provider, check the request counter:

```sh
curl -fsS http://127.0.0.1:8081/metrics | grep -E 'openbao_kms_grpc_requests_total\{method="(encrypt|decrypt)"'
```

The encrypt counter increases when you write a Secret. Decrypt counts can stay
flat because the API server serves many reads from its cache.

## Step 4: Clean up

```sh
kubectl delete secret openbao-kms-first-encrypt
```

{{< checklist title="Finish Get started with" >}}
- A Secret reads back through `kubectl`, and etcd stores it with the `k8s:enc:kms:v2:<provider-name>:` prefix and no plaintext.
- Every control-plane node reports `/ready` and the same `key_id` hash.
- Every API server uses the same KMS-first configuration with `identity` second.
- The probe reads successfully through each API server directly.
- Your identity values and the provider identity fingerprint are recorded in configuration management.
{{< /checklist >}}

Next, plan for day 2: key rotation in [Operate: Rotation](/docs/operate/rotation/)
and recovery in [Operate: Disaster recovery](/docs/operate/disaster-recovery/).
