---
title: "Kubernetes Encryption Config"
description: "Wire bao-kms-provider into the Kubernetes API server through EncryptionConfiguration, restart or reload the API server, and migrate existing API resources."
weight: 40
---

# Kubernetes Encryption Config

Complete [Install](/getting-started/install/) and confirm that
`bao-kms-provider doctor` succeeds on every control-plane node. Then configure
the API server to consume `EncryptionConfiguration` and migrate existing
resources to KMS-encrypted storage.

## Prerequisites

- The provider is running on every control-plane node and exposes its Unix socket at the path documented in the provider configuration file.
- `bao-kms-provider doctor` succeeds with the active configuration.
- The Kubernetes API server has read access to its `EncryptionConfiguration` file.
- The API server runtime user can connect to the provider socket through the `openbao-kms-socket` group (see [Deployment: Linux Identity Model](/deployment/linux-identity-model/)).

## Write The EncryptionConfiguration

The minimal `EncryptionConfiguration` for a fresh enablement keeps the `identity` provider as a fallback so existing plaintext data remains readable during migration:

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

A maintained sample lives at `deploy/kubernetes/encryption-config.yaml` in the repository.

Required values:

| Field | Constraint |
|---|---|
| `providers[].kms.apiVersion` | Always `v2`. KMS v1 is not implemented. |
| `name` | Identity-bearing. Must match `transit.keyIdScope.providerName` in the provider configuration. Do not change after encryption begins. |
| `endpoint` | Must use the `unix://` scheme and match `server.socketPath` in the provider configuration. |
| `timeout` | Start with `3s`. Tighten this value only after benchmark and failure-mode testing; see [Reference: EncryptionConfiguration](/reference/encryption-config/). |
| `resources` | Start narrow (`secrets`). Common second step is to add `configmaps`. |

Custom resource definitions (CRDs) can be encrypted with the same provider.
Plan resource selection deliberately. Size, read and write volume, and recovery
impact all change as the encrypted set grows.

## Configure The Kubernetes API Server

Place the file on every control-plane node, then point `kube-apiserver` at it:

```text
--encryption-provider-config=/etc/kubernetes/encryption-config.yaml
```

To enable in-place reload of the file when its contents change without restarting the API server, also set:

```text
--encryption-provider-config-automatic-reload=true
```

The API server depends on a healthy provider when a configuration change takes
effect. A misspelled provider name or unreachable socket causes KMS Status
failures and then encrypt or decrypt errors. Treat reload as a faster restart.
The API server applies the new configuration immediately. It reports errors on
the next encrypt or decrypt call, not while validating the file.

## Restart Or Reload The API Server

If `--encryption-provider-config-automatic-reload=true` is set, the API server picks up changes to the configuration file in place. Otherwise, restart `kube-apiserver` once on each control-plane node.

After the API server has the new configuration:

1. Confirm the API server log records that the KMS provider is healthy.
2. Confirm `kubectl get nodes` succeeds.
3. Confirm a probe Secret can be created and read back:

   ```sh
   kubectl create secret generic openbao-kms-bootstrap-probe \
     --from-literal=value='probe'
   kubectl get secret openbao-kms-bootstrap-probe \
     -o jsonpath='{.data.value}' | base64 -d
   ```

   Expected output: `probe`.

End-to-end encryption verification, including etcd inspection, `key_id`
stability, and additional authenticated data (AAD) shape, belongs to the next
page. See [First Encrypt](/getting-started/first-encrypt/).

## Migrate Existing Resources

Kubernetes encryption applies on write. Existing objects are not rewritten when
the configuration changes. When enabling the provider on an existing cluster,
rewrite every targeted resource to replace its etcd payload with KMS-encrypted
ciphertext.

For Secrets:

```sh
kubectl get secrets --all-namespaces -o json | kubectl replace -f -
```

Run an equivalent rewrite for every resource type in the `resources` list. The exact command differs per resource; the pattern is `kubectl get <resource> --all-namespaces -o json | kubectl replace -f -`.

After rewriting:

1. Restart `kube-apiserver` on one control-plane node and confirm reads still succeed.
2. Repeat across the remaining control-plane nodes.
3. Verify a sample of objects in etcd to confirm payloads no longer contain plaintext. The [First Encrypt](/getting-started/first-encrypt/) page describes the etcd inspection technique.

## Remove The Identity Fallback

Once every targeted resource has been rewritten and verified, remove the `identity` provider from the configuration:

```yaml
providers:
  - kms:
      apiVersion: v2
      name: openbao-kms-workload-a
      endpoint: unix:///run/openbao-kms/kms.sock
      timeout: 3s
```

Reload or restart the API server. After this point, any object targeted by the configuration that has not been rewritten will fail to decrypt.

Do not remove the fallback before the migration verification on the previous step has completed. Removing it too early breaks reads of plaintext objects that were not migrated.

## Read Next

1. [First Encrypt](/getting-started/first-encrypt/) for the end-to-end smoke test.
2. [Operations: Rotation](/operations/rotation/) once encryption is live and the cluster is in steady state.
