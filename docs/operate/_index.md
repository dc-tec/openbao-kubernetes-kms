---
title: Operate
description: "Rotate keys, upgrade, recover, and troubleshoot a provider that already encrypts cluster data."
eyebrow: Operate
weight: 30
---

These runbooks assume the provider runs on every control-plane node and the
API server encrypts through it. Before you change rotation, recovery, or
upgrade state, confirm `doctor` passes on every node:

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/openbao-kms/encryption-config.yaml
```
