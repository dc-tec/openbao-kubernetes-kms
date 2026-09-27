---
title: Enable encryption
description: "Stage KMS readers on every API server before enabling encrypted writes in a fresh preview cluster."
eyebrow: Get started · Step 7
weight: 80
verifiedBy:
  - deploy/kubernetes/encryption-config.yaml
  - hack/tools/harvester_lab/main.go
  - test/dev-env/scripts/enable-kms.sh
  - test/e2e/kind_smoke_test.go
---

This procedure enables encryption of new Secrets in a fresh, disposable
preview cluster. It keeps plaintext objects readable through `identity`.
It does not migrate existing encrypted data or retire old providers.

## Before you begin

- Run the provider on every control-plane node and check its health.
- Record the same provider identity on every node.
- Identify every API-server endpoint and a kubeconfig that can reach each one
  directly, with valid TLS verification.
- Inspect each API server's arguments. If `--encryption-provider-config`
  already points to a configuration, stop. Do not overwrite it with this
  fresh-install example.
- Save the API-server manifests or service configuration so you can restore
  the configuration before KMS writes begin.

A load-balanced `kubectl` request can reach a different node. The checks below
must target each API server directly.

## Step 1: Stage the reader configuration

Write `/etc/kubernetes/openbao-kms/encryption-config.yaml` on every
control-plane node. Put `identity` first so writes remain plaintext while
servers acquire the KMS reader:

```yaml
apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
    providers:
      - identity: {}
      - kms:
          apiVersion: v2
          name: openbao-kms-workload-a
          endpoint: unix:///run/openbao-kms/kms.sock
          timeout: 3s
```

Set `kms.name` to `transit.keyIdScope.providerName` and `kms.endpoint` to
`unix://` plus `server.socketPath`. Keep those values identical across the
configuration files. KMS v1 is not implemented.

Run the configuration check on each node:

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/openbao-kms/encryption-config.yaml
```

Run it with the provider's runtime user and groups where possible. A root
check does not prove runtime file access. Expect a warning while `identity`
is first: writes remain plaintext at this stage. Resolve failed checks before
continuing. `doctor` does not establish API-server integration.

## Step 2: Configure every API server

On one control-plane node at a time, add this argument to `kube-apiserver`:

```text
--encryption-provider-config=/etc/kubernetes/openbao-kms/encryption-config.yaml
```

The API server needs read access to the file and permission to connect to the
provider socket. For a kubeadm static pod, mount both host directories:

```yaml
spec:
  containers:
    - name: kube-apiserver
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

This fragment contains additions to the existing manifest. Preserve its
existing arguments, mounts, and volumes. Kubelet restarts the API server after
a manifest change. Also record the flag and mounts in your kubeadm
configuration so a later `kubeadm upgrade` preserves them.

For a host service, add the argument and restart the service. Its user must
have socket access through `openbao-kms-socket`.

After each restart, check that node directly:

```sh
API_SERVER=https://control-plane-1.example.com:6443
kubectl --server="${API_SERVER}" get --raw='/readyz?verbose'
kubectl --server="${API_SERVER}" get secrets --all-namespaces -o name
```

Require API-server readiness, including a healthy KMS check, and successful
reads. Inspect the API-server log if either fails. Do not proceed to the next
node until the changed node is healthy.

**Complete this step on every API server before enabling KMS writes.** An
unchanged API server cannot decrypt the new ciphertext.

Before Step 3, rollback consists of restoring the saved API-server
configuration. No KMS writes have been enabled by this procedure yet.

## Step 3: Enable KMS writes

After every server has the KMS reader, change the provider order on one node
at a time to:

```yaml
providers:
  - kms:
      apiVersion: v2
      name: openbao-kms-workload-a
      endpoint: unix:///run/openbao-kms/kms.sock
      timeout: 3s
  - identity: {}
```

Restart that API server, or wait for a successful configuration reload if you
configured `--encryption-provider-config-automatic-reload=true`. Check its
readiness and read existing Secrets directly before changing the next node.
At the end, every API server must use the same KMS-first configuration.

If a problem occurs after KMS writes begin, keep the KMS reader on every
server. You can restore `identity` to the first position to resume plaintext
writes, but you must preserve the KMS entry, provider identity, and OpenBao
key material to read ciphertext already written. Do not restore a
configuration with no KMS reader.

## Step 4: Check a write through every API server

Create a probe through one API server, then read it through each endpoint:

```sh
kubectl --server="${API_SERVER}" create secret generic openbao-kms-bootstrap-probe \
  --from-literal=value=probe
kubectl --server="${API_SERVER}" get secret openbao-kms-bootstrap-probe \
  -o jsonpath='{.data.value}' | base64 -d
```

The read prints `probe`. Change `API_SERVER` for every node and repeat the
read. To check writes through the other servers, delete the probe and repeat
creation through the next endpoint, then repeat the reads through every node.
Delete the probe when finished.

Keep `identity` as the second provider throughout this preview evaluation.
Existing plaintext objects remain readable; they are not rewritten by this
procedure. A sample probe does not prove complete encryption of the cluster.

Continue with [Verify encryption](/docs/get-started/verify/) to inspect the
stored probe and provider signals.
