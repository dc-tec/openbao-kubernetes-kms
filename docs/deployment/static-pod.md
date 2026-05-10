---
title: "Static Pod Deployment"
description: "Static pod manifest, image preload, host preparation, and bootstrap risk profile for running bao-kms-provider as a kubelet-managed pod."
weight: 30
---

# Static Pod Deployment

Static pod deployment is appropriate for kubeadm-style environments and image-based control-plane management. It has different bootstrap risks than systemd.

For the model selection rationale see [Deployment: Choosing A Model](/deployment/choosing-a-model/). For the user, group, and file ownership model see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

## Constraints

A static pod manifest is read from the host filesystem by kubelet. Static pods cannot depend on Kubernetes API objects such as ConfigMaps, Secrets, or ServiceAccounts.

The plugin static pod mounts everything it needs from the host:

- configuration file,
- CA bundle,
- JWT file,
- runtime socket directory,
- optional local state directory.

## Example Manifest

The maintained sample manifest lives at `deploy/static-pod/bao-kms-provider.yaml` in the repository. Replace the image digest and the supplemental group GID before deploying.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: bao-kms-provider
  namespace: kube-system
spec:
  hostNetwork: true
  priorityClassName: system-node-critical
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    supplementalGroups:
      - 1234
  containers:
    - name: bao-kms-provider
      image: ghcr.io/dc-tec/bao-kms-provider@sha256:0000000000000000000000000000000000000000000000000000000000000000
      imagePullPolicy: IfNotPresent
      args:
        - serve
        - --config=/etc/openbao-kms/config.yaml
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
      volumeMounts:
        - name: config
          mountPath: /etc/openbao-kms/config.yaml
          readOnly: true
        - name: tls
          mountPath: /etc/openbao-kms/tls
          readOnly: true
        - name: jwt
          mountPath: /var/lib/openbao-kms/identity.jwt
          readOnly: true
        - name: run
          mountPath: /run/openbao-kms
        - name: state
          mountPath: /var/lib/openbao-kms/state
  volumes:
    - name: config
      hostPath:
        path: /etc/openbao-kms/config.yaml
        type: File
    - name: tls
      hostPath:
        path: /etc/openbao-kms/tls
        type: Directory
    - name: jwt
      hostPath:
        path: /var/lib/openbao-kms/identity.jwt
        type: File
    - name: run
      hostPath:
        path: /run/openbao-kms
        type: Directory
    - name: state
      hostPath:
        path: /var/lib/openbao-kms/state
        type: Directory
```

The final manifest depends on the host socket group GID and the released image digest. The sample uses UID and GID `65532:65532`, matching the distroless non-root image user.

## Image Availability

For air-gapped or bootstrap-sensitive control planes, preload the image on every control-plane node:

- use immutable image digests,
- avoid `Always` pulls in recovery-sensitive deployments,
- keep the previous image available for rollback,
- document image import steps for node replacement.

## Host Preparation

Every control-plane node must have:

```text
/etc/openbao-kms/config.yaml
/etc/openbao-kms/tls/ca.crt
/var/lib/openbao-kms/identity.jwt
/var/lib/openbao-kms/state
/run/openbao-kms
```

The API server must be able to access the socket created under `/run/openbao-kms`. The container user must own the socket directory, or an equally narrow provider-only identity must be the only writer. The API server's socket access group needs execute permission on the directory and write permission on `kms.sock`; it must not have write permission on the directory itself.

The provider configuration used by the static pod sets `server.socketGroup` to the same numeric host GID listed in `supplementalGroups`. See `deploy/config/provider-static-pod.yaml` for the matching configuration sample.

## kubeadm Placement

Typical kubeadm static pod path:

```text
/etc/kubernetes/manifests/bao-kms-provider.yaml
```

The kubelet watches this directory and starts the static pod.

## Bootstrap Risks

Static pod mode depends on:

- kubelet,
- the container runtime,
- local image availability,
- hostPath mounts,
- container networking and DNS,
- file permissions inside the container.

If kubelet or the container runtime is broken, the KMS plugin may not start and the API server may be unable to decrypt existing resources.

The provider retries its initial status probe for `bootstrap.graceTimeout` before exiting. Static pod deployments should keep this enabled because the JWT file, container networking, DNS, OpenBao availability, and clock sync can settle after the container process starts.

For single-node control planes, systemd is usually safer. See [Deployment: Choosing A Model](/deployment/choosing-a-model/).

## Verification

Before enabling API server encryption:

1. Place the static pod manifest under `/etc/kubernetes/manifests/`.
2. Confirm the pod is running through kubelet or container runtime tooling.
3. Confirm `/run/openbao-kms/kms.sock` exists on the host.
4. Run `bao-kms-provider doctor` on the host or in an equivalent debug container.
5. Confirm `kube-apiserver` can connect to the socket.

## Source References

- [Kubernetes static Pods](https://kubernetes.io/docs/tasks/configure-pod-container/static-pod/)
- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
