# Deployment Artifacts

Deployment and packaging samples:

- `config/provider-systemd.yaml`: host-service provider config sample.
- `config/provider-static-pod.yaml`: static-pod provider config sample using a numeric socket GID.
- `systemd/bao-kms-provider.service`: hardened sample systemd unit.
- `static-pod/bao-kms-provider.yaml`: kubeadm-compatible static pod sample.
- `kubernetes/encryption-config.yaml`: Kubernetes KMS v2 `EncryptionConfiguration` sample.
- `package/linux`: nFPM, `sysusers.d`, `tmpfiles.d`, and maintainer-script inputs for `.deb` and `.rpm` packages.
- `package/bundles`: deterministic tarball bundle README inputs for systemd and static-pod release bundles.
- `harvester/openbao-kms-lab`: local-only Harvester VM lab chart for kubeadm validation outside public CI.
- `opentofu/openbao-kubernetes-kms`: OpenTofu module skeleton for OpenBao Transit engine, key, and provider policy setup.

Replace sample OpenBao addresses, identity-bearing fields, image digests, and host socket GIDs before deployment.
