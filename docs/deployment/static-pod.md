# Static Pod Deployment

Static pod deployment is useful for kubeadm-style environments and image-based control-plane management. It has different bootstrap risks than systemd.

## Constraints

A static Pod manifest is read from the host filesystem by kubelet. Static Pods cannot depend on Kubernetes API objects such as ConfigMaps, Secrets, or ServiceAccounts.

Therefore the plugin static pod must mount all required files from the host:

- config file,
- CA bundle,
- JWT file,
- runtime socket directory,
- optional local state directory.

## Example Manifest

Example planning manifest:

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
      - 1234 # replace with the host GID allowed to write /run/openbao-kms
  containers:
    - name: bao-kms-provider
      image: ghcr.io/example/bao-kms-provider:v0.1.0
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
        type: DirectoryOrCreate
    - name: state
      hostPath:
        path: /var/lib/openbao-kms/state
        type: Directory
```

The final manifest will depend on image UID/GID, host permissions, and distribution-specific API server user/group behavior.

## Image Availability

For air-gapped or bootstrap-sensitive control planes, preload the image on every control-plane node.

Recommended:

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

The API server must be able to access the socket created under `/run/openbao-kms`.
The container user or one of its supplemental groups must be able to create the socket in that directory.

## kubeadm Placement

Typical kubeadm static pod path:

```text
/etc/kubernetes/manifests/bao-kms-provider.yaml
```

The kubelet watches this directory and starts the static pod.

## Bootstrap Risks

Static pod mode depends on:

- kubelet,
- container runtime,
- local image availability,
- hostPath mounts,
- container networking and DNS,
- file permissions inside the container.

If kubelet or the container runtime is broken, the KMS plugin may not start and the API server may be unable to decrypt existing resources.

For single-node control planes, systemd mode is usually safer.

## Verification

Before enabling API server encryption:

1. Place the static pod manifest.
2. Confirm the pod is running through kubelet or container runtime tooling.
3. Confirm `/run/openbao-kms/kms.sock` exists on the host.
4. Run `doctor` on the host or in an equivalent debug container.
5. Confirm kube-apiserver can connect to the socket.

## Source References

- [Kubernetes static Pods](https://kubernetes.io/docs/tasks/configure-pod-container/static-pod/)
- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
