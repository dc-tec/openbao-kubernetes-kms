---
title: Verify encryption
description: "Confirm that etcd stores the probe Secret as KMS v2 ciphertext and that every provider reports the same healthy key."
eyebrow: Get started · Step 7
weight: 70
verifiedBy:
  - test/dev-env/scripts/verify-kms.sh
  - internal/metrics
  - internal/health
---

A Secret that reads back through `kubectl` proves only that the API server
works. This page checks that the `openbao-kms-first-encrypt` probe from
[Enable encryption](/docs/get-started/enable-encryption/#step-4-check-a-write-through-every-api-server)
is stored as ciphertext and that every provider is healthy. It does not prove
complete encryption of existing data. Keep the `identity` reader for plaintext
objects in this preview evaluation.

## Step 1: Check the stored value in etcd

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

## Step 2: Check the provider on every node

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

Run `probe` through each node's actual Unix socket, using the intended socket
client identity. For a systemd installation:

```sh
sudo -u openbao-kms bao-kms-provider probe --socket /run/openbao-kms/kms.sock --output json
```

For static pods, use the UID and groups shown in
[Run as a static pod](/docs/get-started/static-pod/#step-8-start-the-static-pod).
All Status, Encrypt, and Decrypt checks must pass. Record the successful
`kms.key_id` value from one node, then require it on the other nodes with
`--expected-key-id`. Different active keys can indicate identity drift or
rotation that has not converged. Do not change lineage values to force a match.
A local `doctor` pass, a root-only permission check, and a skipped check do not
substitute for this live result.

To confirm that encrypt calls reach the provider, check the request counter:

```sh
curl -fsS http://127.0.0.1:8081/metrics | grep -E 'openbao_kms_grpc_requests_total\{method="(encrypt|decrypt)"'
```

The encrypt counter increases when you write a Secret. Decrypt counts can stay
flat because the API server serves many reads from its cache.

## Step 3: Clean up

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
