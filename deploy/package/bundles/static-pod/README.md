# Static Pod Release Bundle

This bundle contains host-filesystem inputs for kubelet-managed static pod
deployments. It is not a Helm chart and does not require a working Kubernetes
API server.

It contains:

- `static-pod/bao-kms-provider.yaml`
- `config/provider-static-pod.yaml`
- `kubernetes/encryption-config.yaml`
- `image-ref.txt`

Before placing the manifest under `/etc/kubernetes/manifests`, replace sample
OpenBao addresses, identity-bearing fields, socket GIDs, and the image digest
with values from the release evidence and the target control-plane host.
