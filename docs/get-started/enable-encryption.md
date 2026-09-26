---
title: Enable encryption
description: "Point kube-apiserver at the provider through an EncryptionConfiguration, restart it one node at a time, and rewrite existing Secrets so they are stored encrypted."
eyebrow: Get started · Step 7
weight: 80
verifiedBy:
  - deploy/kubernetes/encryption-config.yaml
  - hack/tools/harvester_lab/main.go
  - test/dev-env/scripts/enable-kms.sh
  - test/e2e/kind_smoke_test.go
---

At the end of this step, the API server encrypts new and rewritten Secrets
through the provider, and still reads any plaintext data through the `identity`
fallback.

## Before you begin

- The provider runs on every control-plane node and `doctor` passes, from
  [Run with systemd](/docs/get-started/systemd/) or
  [Run as a static pod](/docs/get-started/static-pod/).
- You can restart `kube-apiserver` on each control-plane node, one at a time.

## Step 1: Write the EncryptionConfiguration

Write this file to `/etc/kubernetes/openbao-kms/encryption-config.yaml` on
every control-plane node, with your provider name in `name`:

```yaml
apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
    providers:
      - kms:
          apiVersion: v2
          name: openbao-kms-workload-a
          endpoint: unix:///run/openbao-kms/kms.sock
          timeout: 3s
      - identity: {}
```

| Field | Rule |
|---|---|
| `kms.apiVersion` | Always `v2`. KMS v1 is not implemented. |
| `kms.name` | Identity-bearing. Equal to `transit.keyIdScope.providerName` in the provider configuration. Never change it after encryption begins. |
| `kms.endpoint` | `unix://` plus `server.socketPath` from the provider configuration. |
| `kms.timeout` | Start with `3s`. Change it only after benchmark and failure testing; see [Reference: EncryptionConfiguration](/docs/reference/encryption-config/). |
| `identity: {}` | Keeps existing plaintext data readable until Step 5 and verification are complete. |
| `resources` | Start with `secrets`. Add more resource types later, one at a time. |

If you ran [`init`](/docs/get-started/plan-values/#generate-the-files-with-init),
copy `generated/encryption-config.yaml` instead of writing the file by hand.

Check the file against the provider configuration on each node:

```sh
sudo bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/openbao-kms/encryption-config.yaml
```

`doctor` exits with status `0` without a `[fail]` check. It fails if the
provider name or endpoint does not match the provider configuration.

## Step 2: Point kube-apiserver at the file

The API server needs the `--encryption-provider-config` flag, read access to
the file, and access to the provider socket.

**kubeadm-style control plane.** Edit
`/etc/kubernetes/manifests/kube-apiserver.yaml` on the first control-plane
node. Add the flag, and mount the configuration directory and the socket
directory from the host. This fragment shows only the additions:

```yaml
spec:
  containers:
    - command:
        - kube-apiserver
        - --encryption-provider-config=/etc/kubernetes/openbao-kms/encryption-config.yaml
      volumeMounts:
        - name: openbao-kms-encryption
          mountPath: /etc/kubernetes/openbao-kms
          readOnly: true
        - name: openbao-kms-run
          mountPath: /run/openbao-kms
  volumes:
    - name: openbao-kms-encryption
      hostPath:
        path: /etc/kubernetes/openbao-kms
        type: Directory
    - name: openbao-kms-run
      hostPath:
        path: /run/openbao-kms
        type: Directory
```

Kubelet restarts the API server when the manifest changes. `kubeadm upgrade`
regenerates this manifest, so also record the flag and both mounts in the
kubeadm `ClusterConfiguration` under `apiServer.extraArgs` and
`apiServer.extraVolumes`.

**API server as a host service.** Add the flag to the service's arguments and
make sure its user can read the file and connect to the socket through the
`openbao-kms-socket` group, then restart the service. See
[Security: Linux identity model](/docs/security/linux-identity-model/).

To have the API server reload the file when it changes, also set
`--encryption-provider-config-automatic-reload=true`. A reload applies the new
configuration immediately and reports mistakes only on the next encrypt or
decrypt call, so treat every change like a restart.

## Step 3: Confirm the first node

On the node you changed, confirm the API server is back and encrypts through
the provider:

```sh
kubectl get nodes
kubectl create secret generic openbao-kms-bootstrap-probe --from-literal=value=probe
kubectl get secret openbao-kms-bootstrap-probe -o jsonpath='{.data.value}' | base64 -d
```

The last command prints `probe`. If the API server does not come back, revert
the manifest change and check the API server log for KMS errors; see
[Operate: Troubleshooting](/docs/operate/troubleshooting/).

## Step 4: Change the remaining nodes

Repeat Step 2 and Step 3 on each remaining control-plane node, one node at a
time. Every node must use the identical `EncryptionConfiguration`.

## Step 5: Rewrite existing Secrets

Kubernetes encrypts on write, so Secrets created before this step are still
stored in plaintext. Rewrite every Secret to store it through the provider:

```sh
kubectl get secrets --all-namespaces -o json | kubectl replace -f -
```

Run the same rewrite for every other resource type you add to `resources`.
Then restart `kube-apiserver` on one control-plane node and confirm reads
still succeed before you restart the others.

The `identity` fallback stays in place until
[Verify encryption](/docs/get-started/verify/) confirms that etcd holds only
ciphertext.
