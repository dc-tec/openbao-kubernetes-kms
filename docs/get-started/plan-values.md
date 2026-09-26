---
title: Plan identity values
description: "Choose the names and identity values that OpenBao, the provider configuration, and the Kubernetes EncryptionConfiguration must share, before you create anything."
eyebrow: Get started · Step 3
weight: 30
verifiedBy:
  - internal/config/config.go
  - internal/config/validation.go
  - deploy/config/provider-systemd.yaml
  - deploy/kubernetes/encryption-config.yaml
---

A working deployment repeats the same values in three places: the OpenBao
setup, the provider configuration on every control-plane node, and the
Kubernetes `EncryptionConfiguration`. Choose them once, record them, and reuse
them on every later page.

{{< callout type="warning" title="Identity-bearing values are permanent" >}}
Values marked identity-bearing feed the Kubernetes `key_id` and the additional
authenticated data (AAD) bound to every encrypted object. Changing one after
encryption begins can make existing data unreadable. Every control-plane node
must use identical values.
{{< /callout >}}

## Choose the values

| Value | Identity-bearing | How to choose it | Example |
|---|---|---|---|
| Cluster ID | Yes | A stable label for this Kubernetes cluster that you own, such as an inventory name. | `workload-a` |
| Provider name | Yes | The KMS provider name Kubernetes stores in every etcd envelope prefix. Keep it unique per cluster. | `openbao-kms-workload-a` |
| OpenBao instance ID | Yes | A stable label for the OpenBao cluster. Do not use its URL or hostname, which can change. | `bao-prod-a` |
| OpenBao namespace | Yes | Only when one OpenBao cluster serves several Kubernetes clusters. Leave empty for the root namespace. | `admin/workload-a` |
| Transit mount path | Yes | A dedicated Transit mount for Kubernetes KMS keys. | `transit` |
| Transit mount ID | Yes | A stable label for that mount. Do not use the OpenBao mount accessor, which changes on remount or restore. | `transit-prod-primary` |
| Transit key name | Yes | One key per cluster or trust domain, as a single path segment without `/` or `%`. | `k8s-workload-a-etcd` |
| Key lineage ID | Yes | A random, non-secret ID generated once when the Transit key is created. You generate it in [Prepare OpenBao](/docs/get-started/openbao/#step-3-capture-the-key-lineage-id). | `7d34fb7df15f4e4c95d6c2a50fe90d84` |
| JWT auth mount path | No | A dedicated JWT auth mount for this cluster's providers. | `k8s-workload-a-jwt` |
| JWT role | No | The OpenBao role the provider logs in with. | `openbao-kms-control-plane` |
| OpenBao policy name | No | The least-privilege policy attached to that role. | `openbao-kms-workload-a` |
| JWT issuer | No | The issuer of the provider's host JWT. It must stay reachable when the protected API server is down. | `https://issuer.example.internal` |
| JWT audience and subject | No | The audience and subject the issuer puts in the provider JWT. | `bao-kms-provider`, `system:openbao-kms:workload-a` |
| Socket path | No | The Unix socket the API server connects to. Keep the default. | `/run/openbao-kms/kms.sock` |

Do not derive identity values from mutable topology such as URLs, node names,
or mount accessors. For the reasoning, see
[Reference: Key ID and AAD](/docs/reference/key-id-and-aad/).

## Record the values

Keep the values in platform configuration management. The later pages use
these shell variables in their commands, so set them in the shell you use for
OpenBao administration:

```sh
CLUSTER_ID=workload-a
PROVIDER_NAME=openbao-kms-workload-a
OPENBAO_INSTANCE_ID=bao-prod-a
TRANSIT_MOUNT=transit
TRANSIT_MOUNT_ID=transit-prod-primary
KEY_NAME=k8s-workload-a-etcd
JWT_MOUNT=k8s-workload-a-jwt
JWT_ROLE=openbao-kms-control-plane
POLICY_NAME=openbao-kms-workload-a
JWT_ISSUER=https://issuer.example.internal
JWT_AUDIENCE=bao-kms-provider
JWT_SUBJECT=system:openbao-kms:workload-a
```

If the cluster uses an OpenBao namespace, also export it for the `bao` CLI:

```sh
export BAO_NAMESPACE=admin/workload-a
```

## Where each value goes

| Value | OpenBao | Provider configuration | `EncryptionConfiguration` |
|---|---|---|---|
| Cluster ID | | `transit.keyIdScope.clusterId` | |
| Provider name | | `transit.keyIdScope.providerName` | `providers[].kms.name` |
| OpenBao instance ID | | `openbao.instanceId` | |
| OpenBao namespace | Namespace for all commands | `openbao.namespace` | |
| Transit mount path | Mount path, policy paths | `transit.mountPath` | |
| Transit mount ID | | `transit.keyIdScope.transitMountId` | |
| Transit key name | Key, policy paths | `transit.keyName` | |
| Key lineage ID | | `transit.keyIdScope.keyLineageId` | |
| JWT auth mount path | Auth mount | `auth.jwt.mountPath` (with the `auth/` prefix) | |
| JWT role | Role | `auth.jwt.role` | |
| OpenBao policy name | Policy, role `token_policies` | | |
| JWT issuer, audience, subject | Auth config and role bindings | `auth.jwt.expectedIssuer`, `expectedAudience`, `expectedSubject` | |
| Socket path | | `server.socketPath` | `providers[].kms.endpoint` (with the `unix://` prefix) |

## Provider configuration

Each control-plane node's provider configuration starts from the sample
installed with the release. Replace the sample values in these fields with
your recorded values, plus the OpenBao address and TLS server name. This
fragment shows only the fields you change:

```yaml
openbao:
  address: https://bao.example.internal:8200
  tlsServerName: bao.example.internal
  namespace: ""                            # OpenBao namespace, if any
  instanceId: bao-prod-a                   # OpenBao instance ID
auth:
  jwt:
    mountPath: auth/k8s-workload-a-jwt     # auth/ + JWT auth mount path
    role: openbao-kms-control-plane        # JWT role
    expectedIssuer: https://issuer.example.internal
    expectedAudience:
      - bao-kms-provider
    expectedSubject: system:openbao-kms:workload-a
transit:
  mountPath: transit                       # Transit mount path
  keyName: k8s-workload-a-etcd             # Transit key name
  keyIdScope:
    providerName: openbao-kms-workload-a   # Provider name
    clusterId: workload-a                  # Cluster ID
    transitMountId: transit-prod-primary   # Transit mount ID
    keyLineageId: "7d34fb7df15f4e4c95d6c2a50fe90d84"
```

Static-pod deployments also set `server.socketGroup` to the numeric host group
ID of `openbao-kms-socket`.

The provider prints an identity fingerprint over the identity-bearing values
when you run `bao-kms-provider config`. Record it during rollout; every
control-plane node must print the same fingerprint. For every field, see
[Reference: Configuration](/docs/reference/configuration/#identity-bearing-fields).
