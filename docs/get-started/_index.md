---
title: Get started
description: "Take one Kubernetes cluster from no provider to Secrets stored as OpenBao Transit ciphertext in etcd."
eyebrow: Get started
weight: 10
hideChildren: true
---

`bao-kms-provider` runs on every control-plane node and lets `kube-apiserver`
encrypt Secrets and other resources with an OpenBao Transit key before they
reach etcd. These steps set it up for one cluster, in order.

{{< callout type="warning" title="Preview release" >}}
The current release line is a preview. Use it for labs, staging, and
evaluation. Do not protect production control planes with it.
{{< /callout >}}

## The path

| Step | Page | Outcome |
|---|---|---|
| 1 | [What the provider does](/docs/get-started/overview/) | You know what the provider protects, what it does not, and whether your platform fits. |
| 2 | [Choose a deployment model](/docs/get-started/deployment-model/) | You pick systemd or static pod for the control-plane nodes. |
| 3 | [Plan identity values](/docs/get-started/plan-values/) | You have recorded every name and identity value the later steps reuse. |
| 4 | [Prepare OpenBao](/docs/get-started/openbao/) | OpenBao holds the Transit key, the policy, and the JWT role. |
| 5 | [Download the release](/docs/get-started/download/) | You have a verified artifact for your deployment model. |
| 6 | [Run with systemd](/docs/get-started/systemd/) or [Run as a static pod](/docs/get-started/static-pod/) | The provider runs on every control-plane node and passes `doctor`. |
| 7 | [Enable encryption](/docs/get-started/enable-encryption/) | The API server encrypts through the provider, and existing Secrets are rewritten. |
| 8 | [Verify encryption](/docs/get-started/verify/) | etcd holds only ciphertext, and the identity fallback is removed. |

## What you need

- A Kubernetes `1.34` or `1.35` cluster whose control-plane nodes you can
  administer. See [Reference: Compatibility](/docs/reference/compatibility/)
  for the tested matrix.
- An OpenBao `2.6` cluster reachable over HTTPS from every control-plane node,
  and an administrator token for it.
- A JWT issuer for the provider's host credential that keeps working when the
  protected API server is down.
- On your workstation: the `bao` CLI, `cosign`, and an authenticated GitHub
  CLI (`gh`).

To try the provider without a cluster or OpenBao of your own, contributors
maintain a local lab that builds the provider from source and wires it into a
Kind cluster with one command. See [Contribute: Local lab](/contribute/local-lab/).

After Get started, continue with [Operate](/docs/operate/) for rotation,
upgrades, and recovery, and [Configure](/docs/configure/) for auth and policy
variants and monitoring.
