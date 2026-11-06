---
title: Generate installation files
description: "Enter shared identity values once and generate a reviewable installation directory with init."
eyebrow: Get started · Step 3
weight: 30
verifiedBy:
  - cmd/bao-kms-provider/init.go
  - cmd/bao-kms-provider/init_record.go
  - deploy/config/init-values-file.yaml
  - deploy/config/init-values-oauth2.yaml
---

`init` turns one values file into every file that must agree on the provider's
identity: the provider configuration, both encryption configurations, the
OpenBao policy and setup script, and for static pods the manifest. It never
contacts OpenBao or changes a host. Run it on a workstation for a fresh,
disposable evaluation cluster.

## Write the values file

Save this example as `values.yaml` and replace its addresses and identity
values. For native OAuth, start from `deploy/config/init-values-oauth2.yaml`
instead. Do not put a JWT, client secret, or OpenBao token in the values file.

```yaml
# Preview fresh-install values. Replace the example addresses and identities.
# Generate once with --new-key; reuse generated/config.yaml on other nodes.
configVersion: v1alpha1
openbao:
  address: https://bao.example.internal:8200
  tlsServerName: bao.example.internal
  instanceId: bao-prod-a                 # Stable identity, not a hostname.
auth:
  jwt:
    source: file
    mountPath: auth/k8s-workload-a-jwt
    role: openbao-kms-control-plane
    # The host JWT agent must renew without the protected Kubernetes API.
    expectedIssuer: https://issuer.example.internal
    expectedAudience: [bao-kms-provider]
    expectedSubject: system:openbao-kms:workload-a
transit:
  mountPath: transit
  keyName: k8s-workload-a-etcd
  keyIdScope:
    providerName: openbao-kms-workload-a
    clusterId: workload-a
    transitMountId: transit-prod-primary
    # Omit keyLineageId only when --new-key creates it for a new Transit key.
```

Choose each value as follows. Identity-bearing values are part of every
`key_id` and must stay the same on every node for the life of the key.

| Value | Identity-bearing | How to choose it | Example |
|---|---|---|---|
| Cluster ID | Yes | A stable label for this Kubernetes cluster that you own, such as an inventory name. | `workload-a` |
| Provider name | Yes | The KMS provider name Kubernetes stores in every etcd envelope prefix. Keep it unique per cluster. | `openbao-kms-workload-a` |
| OpenBao instance ID | Yes | A stable label for the OpenBao cluster. Do not use its URL or hostname, which can change. | `bao-prod-a` |
| OpenBao namespace | Yes | Only when one OpenBao cluster serves several Kubernetes clusters. Leave empty for the root namespace. | `admin/workload-a` |
| Transit mount path | Yes | A dedicated Transit mount for Kubernetes KMS keys. | `transit` |
| Transit mount ID | Yes | A stable label for that mount. Do not use the OpenBao mount accessor, which changes on remount or restore. | `transit-prod-primary` |
| Transit key name | Yes | One key per cluster or trust domain, as a single path segment without `/` or `%`. | `k8s-workload-a-etcd` |
| Key lineage ID | Yes | A random, non-secret ID generated once when the Transit key is created. `init --new-key` generates it and records it in `config.yaml`. | `7d34fb7df15f4e4c95d6c2a50fe90d84` |
| JWT auth mount path | No | A dedicated JWT auth mount for this cluster's providers. | `k8s-workload-a-jwt` |
| JWT role | No | The OpenBao role the provider logs in with. | `openbao-kms-control-plane` |
| OpenBao policy name | No | The least-privilege policy attached to that role. Default: `openbao-kms-<clusterId>`; override with `--policy-name`. | `openbao-kms-workload-a` |
| JWT issuer | No | The issuer of the provider's host JWT. It must stay reachable when the protected API server is down. | `https://issuer.example.internal` |
| JWT audience and subject | No | The audience and subject the issuer puts in the provider JWT. | `bao-kms-provider`, `system:openbao-kms:workload-a` |
| Socket path | No | The Unix socket the API server connects to. Keep the default. | `/run/openbao-kms/kms.sock` |

Do not derive identity values from mutable topology such as URLs, node names,
or mount accessors. For the reasoning, see
[Reference: Key ID and AAD](/docs/reference/key-id-and-aad/). For every flag
and the checks `init` applies to the values, see
[Reference: CLI](/docs/reference/cli/#init).

## Read the static-pod socket GID

Skip this section for systemd; the package creates the socket group.

A static pod needs the numeric ID of the host group that the API server uses
to reach the provider socket. On each control-plane node, create the group if
it does not exist and print its GID:

```sh
getent group openbao-kms-socket >/dev/null || sudo groupadd --system openbao-kms-socket
getent group openbao-kms-socket | cut -d: -f3
```

Use the same GID on every node where possible. If a node differs, generate its
files separately as shown in [Reuse the identity on other nodes](#reuse-the-identity-on-other-nodes).

## Generate the files

Generate files for a new Transit key:

```sh
bao-kms-provider init --values values.yaml --out generated --new-key
```

For a static pod, add the verified image reference from `image-ref.txt` and the
socket GID:

```sh
bao-kms-provider init --values values.yaml --out generated --new-key \
  --model static-pod --image "$(cat image-ref.txt)" --socket-gid <gid>
```

Run `init` with the binary for your workstation. On macOS, download and verify
`bao-kms-provider_<version>_darwin_<arch>` as described in
[Download the release](/docs/get-started/download/#choose-the-artifact). On
Windows, use the Linux binary inside WSL2.

`init` prints the identity fingerprint and the new lineage ID. It rejects
incompatible values before writing anything and refuses to write into an output
directory that contains files.

## Review and record the output

| File | Review |
|---|---|
| `config.yaml` | Resolved values, host paths, and the generated lineage ID. Keep this as the input for other nodes. |
| `encryption-config-readers.yaml` | Phase 1: `identity` first, KMS second. Used in [Enable encryption](/docs/get-started/enable-encryption/). |
| `encryption-config.yaml` | Phase 2: KMS first, `identity` second. Install only after every API server has the KMS reader. |
| `openbao-policy.hcl` | Provider permissions without key administration. |
| `openbao-setup.sh` | Commands for an OpenBao administrator to review and run once for the new key. |
| `bao-kms-provider.yaml` | Static pod only: pinned image, credential mounts, UID/GID, and socket group. |
| `node-setup.sh` | The phases each node runs as root to create its directories, install its files, check them, and start the provider. |
| `installation.json` | Generator version, image digest when supplied, identity fingerprint, host inputs, file list, and remaining actions. |

Record the identity fingerprint and lineage ID in configuration management.
`installation.json` describes the generated inputs. It contains no credentials
and does not report installation or activation success.

## Reuse the identity on other nodes

The same `generated/` directory serves every node that shares its host paths
and socket GID. For a node that differs, generate from the resolved
configuration and omit `--new-key`:

```sh
bao-kms-provider init --values generated/config.yaml --out generated-node-2
```

For static pods, pass the same `--model` and `--image`, and that node's
`--socket-gid`. Credential and state paths can also differ by node. Every node
must print the same identity fingerprint.

Never generate a new lineage ID for another node using the same Transit key.
`--new-key` rejects an input that already contains a lineage ID. For an existing
key, provide its recorded lineage ID; do not invent a replacement.

Continue with [Prepare OpenBao](/docs/get-started/openbao/).
