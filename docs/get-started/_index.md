---
title: "Get Started"
description: "Confirm the deployment fits, set up OpenBao Transit, install the provider, wire Kubernetes encryption, and verify end-to-end."
weight: 10
---

Use this section when you are new to `bao-kms-provider` or when you need the shortest safe path from first install to a working KMS v2 encryption configuration.

## Recommended Order

1. [Overview](/docs/get-started/overview/) to confirm what the provider does and does not do, and that the OpenBao Transit pattern fits your platform.
2. [Choosing A Model](/docs/get-started/deployment-model/) to decide between systemd and static pod before you install anything.
3. [OpenBao Setup](/docs/get-started/openbao/) to provision the Transit mount, key, policy, and provider authentication.
4. [Install](/docs/get-started/install/) to fetch a verified artifact and validate the local environment.
5. [systemd Deployment](/docs/get-started/systemd/) or [Static Pod Deployment](/docs/get-started/static-pod/) to run the provider on every control-plane node.
6. [Kubernetes Encryption Config](/docs/get-started/enable-encryption/) to write the `EncryptionConfiguration` the Kubernetes API server consumes.
7. [First Encrypt](/docs/get-started/verify/) to run the smoke test and confirm encrypted resources land in etcd as expected.

## Then Move To

- [Operate](/docs/operate/) for rotation, disaster recovery, upgrade, and troubleshooting once the provider is live.
- [Reference](/docs/reference/) when the question becomes behavior-specific instead of workflow-specific.
