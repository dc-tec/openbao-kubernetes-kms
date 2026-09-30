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
| 1 | [Before you begin](/docs/get-started/before-you-begin/) | You know whether the provider fits, and you pick a deployment model and a credential source. |
| 2 | [Download the release](/docs/get-started/download/) | You have one verified release artifact. |
| 3 | [Generate installation files](/docs/get-started/plan-values/) | One values file produces matching configs, policy, OpenBao setup, and an installation record. |
| 4 | [Prepare OpenBao](/docs/get-started/openbao/) | An administrator reviews and runs the generated setup once. |
| 5 | [Run with systemd](/docs/get-started/systemd/) or [Run as a static pod](/docs/get-started/static-pod/) | The provider runs on every control-plane node and passes its checks. |
| 6 | [Enable encryption](/docs/get-started/enable-encryption/) | Every API server reads KMS before any writes it, and a probe Secret reads through each one. |
| 7 | [Verify encryption](/docs/get-started/verify/) | etcd stores the probe as ciphertext and every provider reports the same key. |

## What you need

- A Kubernetes `1.34` or `1.35` cluster whose control-plane nodes you can
  administer. See [Reference: Compatibility](/docs/reference/compatibility/)
  for the tested matrix.
- An OpenBao `2.6` cluster reachable over HTTPS from every control-plane node,
  and an administrator token for it.
- A JWT issuer for the provider's host credential that keeps working when the
  protected API server is down.
- On your workstation: the `bao` CLI, `curl`, and `cosign`. An authenticated
  GitHub CLI (`gh`) is optional, for build provenance checks.

To try the provider without a cluster or OpenBao of your own, contributors
maintain a local lab that builds the provider from source and wires it into a
Kind cluster with one command. See [Contribute: Local lab](/contribute/local-lab/).

After Get started, continue with [Operate](/docs/operate/) for rotation,
upgrades, and recovery, and [Configure](/docs/configure/) for auth and policy
variants and monitoring.
