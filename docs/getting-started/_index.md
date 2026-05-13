---
title: "Start Here"
description: "Confirm the deployment fits, set up OpenBao Transit, install the provider, wire Kubernetes encryption, and verify end-to-end."
weight: 10
browse:
  - "/getting-started/overview"
  - "/getting-started/openbao-setup"
  - "/getting-started/install"
  - "/getting-started/kubernetes-encryption-config"
  - "/getting-started/first-encrypt"
---

# Start Here

Use this section when you are new to `bao-kms-provider` or when you need the shortest safe path from first install to a working KMS v2 encryption configuration.

## Recommended Order

1. [Overview](/getting-started/overview/) to confirm what the provider does and does not do, and that the OpenBao Transit pattern fits your platform.
2. [OpenBao Setup](/getting-started/openbao-setup/) to provision the Transit mount, key, policy, and provider authentication.
3. [Install](/getting-started/install/) to fetch a verified binary and validate the local environment.
4. [Kubernetes Encryption Config](/getting-started/kubernetes-encryption-config/) to write the `EncryptionConfiguration` the Kubernetes API server consumes.
5. [First Encrypt](/getting-started/first-encrypt/) to run the smoke test and confirm encrypted resources land in etcd as expected.

## Then Move To

- [Deployment](/deployment/) to choose between systemd and static-pod and apply the matching identity and hardening.
- [Operations](/operations/) for rotation, disaster recovery, upgrade, and troubleshooting once the provider is live.
- [Reference](/reference/) when the question becomes behavior-specific instead of workflow-specific.
