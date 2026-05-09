# Deployment Artifacts

WS10 deployment and packaging samples:

- `config/provider-systemd.yaml`: host-service provider config sample.
- `config/provider-static-pod.yaml`: static-pod provider config sample using a numeric socket GID.
- `systemd/bao-kms-provider.service`: hardened sample systemd unit.
- `static-pod/bao-kms-provider.yaml`: kubeadm-compatible static pod sample.
- `kubernetes/encryption-config.yaml`: Kubernetes KMS v2 `EncryptionConfiguration` sample.
- `package/linux`: Linux `sysusers.d` and `tmpfiles.d` package snippets.
- `opentofu/openbao-kubernetes-kms`: OpenTofu module skeleton for rendering config and policy artifacts.

Replace sample OpenBao addresses, identity-bearing fields, image digests, and host socket GIDs before deployment.
