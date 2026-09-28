---
title: Generate installation files
description: "Enter shared identity values once and generate a reviewable installation directory with init."
eyebrow: Get started · Step 5
weight: 50
verifiedBy:
  - cmd/bao-kms-provider/init.go
  - cmd/bao-kms-provider/init_record.go
  - deploy/config/init-values-file.yaml
  - deploy/config/init-values-oauth2.yaml
---

Use `init` as the setup path for a fresh, disposable evaluation cluster.
Choose [credentials](/docs/get-started/credentials/) and obtain the matching
[artifact](/docs/get-started/download/) first. These Next instructions require
an unreleased candidate; preview.2 does not contain `init`.

## Generate the files with init

Save this minimal example as `values.yaml` and replace its addresses and
identity values. Use `deploy/config/init-values-oauth2.yaml` for native OAuth
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

Generate files for a new Transit key:

```sh
bao-kms-provider init --values values.yaml --out generated --new-key
```

On a workstation without the Linux binary, use the same candidate image by
its verified digest:

```sh
IMAGE=ghcr.io/dc-tec/bao-kms-provider@sha256:<digest>
docker run --rm --user "$(id -u):$(id -g)" \
  -v "$PWD:/work" -w /work "${IMAGE}" \
  init --values values.yaml --out generated --new-key
```

For a static pod, add `--model static-pod --image "${IMAGE}" --socket-gid <gid>`.
Use the numeric `openbao-kms-socket` group ID from the target host. Set
`--policy-name` only if you need a policy name other than
`openbao-kms-<clusterId>`.

`init` never contacts OpenBao or activates encryption. It refuses to overwrite
an output directory containing files.

Generation uses the values file and documented defaults. It ignores environment
overrides and rejects `--config`, `--log-level`, `--metrics-address`, and
`--health-address`; put those settings in the values file. The generated JWT
role has a 30-minute TTL, so `auth.loginBeforeTokenExpiry` must be less than
30 minutes. `init` rejects incompatible values before writing files.

Use a dedicated policy name made of ASCII letters, digits, underscores, dots,
and hyphens, starting with a letter, digit, or underscore. `root` and `default`
are reserved. JWT role names follow the same path-component rules as Transit
key names. Audience strings are serialized as a JSON array without splitting
commas inside an audience.

For static pods, keep CA and credential files in dedicated directories, outside
the socket and state directories. The generator rejects broad parent mounts
such as `/` and `/etc`, and writable directory overlaps.

## Review and record the output

| File | Review |
|---|---|
| `config.yaml` | Resolved values, host paths, and the generated lineage ID. Keep this as the input for subsequent nodes. |
| `encryption-config-readers.yaml` | Phase 1: `identity` first, KMS second. Stage this on every API server before enabling KMS writes. |
| `encryption-config.yaml` | Phase 2: KMS first, `identity` second. Install only after every API server has the KMS reader. |
| `openbao-policy.hcl` | Provider permissions without key administration. |
| `openbao-setup.sh` | Commands for an OpenBao administrator to review and run once for the new key. |
| `bao-kms-provider.yaml` | Static pod only: pinned image, credential mounts, UID/GID, and socket group. |
| `installation.json` | Generator version and commit, image digest when supplied, identity fingerprint, host inputs, file list, and remaining actions. |

Record the identity fingerprint and lineage ID in configuration management.
The record describes generated inputs; it is not an installation or activation
success report. Credentials are not copied into it.

## Reuse the identity on other nodes

Use `generated/config.yaml` as the resolved values input. Omit `--new-key`:

```sh
bao-kms-provider init --values generated/config.yaml --out generated-node-2
```

Keep the deployment model consistent. For static pods, pass the same image and
the target host's socket GID again; adjust `server.socketGroup` in that node's
values if its GID differs. Credential and state paths can also differ by node.
All nodes must retain the same identity-bearing values and fingerprint.

Never generate a new lineage ID for another node using the same Transit key.
`--new-key` rejects an input that already contains a lineage ID. For an existing
key, provide its recorded lineage ID; do not invent a replacement.

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
| Key lineage ID | Yes | A random, non-secret ID generated once when the Transit key is created. `init --new-key` generates it and records it in `config.yaml`. | `7d34fb7df15f4e4c95d6c2a50fe90d84` |
| JWT auth mount path | No | A dedicated JWT auth mount for this cluster's providers. | `k8s-workload-a-jwt` |
| JWT role | No | The OpenBao role the provider logs in with. | `openbao-kms-control-plane` |
| OpenBao policy name | No | The least-privilege policy attached to that role. | `openbao-kms-workload-a` |
| JWT issuer | No | The issuer of the provider's host JWT. It must stay reachable when the protected API server is down. | `https://issuer.example.internal` |
| JWT audience and subject | No | The audience and subject the issuer puts in the provider JWT. | `bao-kms-provider`, `system:openbao-kms:workload-a` |
| Socket path | No | The Unix socket the API server connects to. Keep the default. | `/run/openbao-kms/kms.sock` |

Do not derive identity values from mutable topology such as URLs, node names,
or mount accessors. For the reasoning, see
[Reference: Key ID and AAD](/docs/reference/key-id-and-aad/).


Continue with [Prepare OpenBao](/docs/get-started/openbao/) to review and run
the generated setup commands.
