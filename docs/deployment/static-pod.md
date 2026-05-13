---
title: "Static Pod Deployment"
description: "Static pod manifest, image preload, host preparation, and bootstrap risk profile for running bao-kms-provider as a kubelet-managed pod."
weight: 30
---

# Static Pod Deployment

Static pod deployment is appropriate for kubeadm-style environments and image-based control-plane management. It keeps the provider under kubelet management alongside the API server. In this model, kubelet, the container runtime, and local image availability become part of the KMS provider boot path.

For the model selection rationale see [Deployment: Choosing A Model](/deployment/choosing-a-model/). For the user, group, and file ownership model see [Deployment: Linux Identity Model](/deployment/linux-identity-model/).

## Constraints

A static pod manifest is read from the host filesystem by kubelet. Static pods cannot depend on Kubernetes API objects such as ConfigMaps, Secrets, or ServiceAccounts. The protected API server may need the KMS provider before those API objects are reachable.

The plugin static pod mounts everything it needs from the host:

- configuration file,
- CA bundle,
- configured auth material such as a JWT file, certificate chain, PKCS#11 PIN file, or SPIFFE Workload API socket,
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
  labels:
    app.kubernetes.io/name: bao-kms-provider
    app.kubernetes.io/component: kms-provider
spec:
  hostNetwork: true
  priorityClassName: system-node-critical
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    supplementalGroups:
      - 1234
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: bao-kms-provider
      image: ghcr.io/dc-tec/bao-kms-provider@sha256:0000000000000000000000000000000000000000000000000000000000000000
      imagePullPolicy: IfNotPresent
      args:
        - serve
        - --config=/etc/openbao-kms/config.yaml
      ports:
        - name: metrics
          containerPort: 8081
          protocol: TCP
        - name: health
          containerPort: 8082
          protocol: TCP
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop:
            - ALL
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
      livenessProbe:
        httpGet:
          host: 127.0.0.1
          path: /live
          port: 8082
        initialDelaySeconds: 5
        periodSeconds: 10
      readinessProbe:
        httpGet:
          host: 127.0.0.1
          path: /ready
          port: 8082
        initialDelaySeconds: 5
        periodSeconds: 10
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

## Pod Hardening

| Setting | Purpose |
|---|---|
| `hostNetwork: true` | Avoids CNI availability as an early-boot dependency. |
| `automountServiceAccountToken: false` | Prevents accidental dependency on protected-cluster ServiceAccount tokens. |
| `runAsNonRoot: true` and `runAsUser: 65532` | Runs as the distroless non-root image user. |
| `supplementalGroups` | Gives the container access to the host socket group without exposing provider auth material to the API server. |
| `seccompProfile: RuntimeDefault` | Uses the runtime default syscall filter. |
| `allowPrivilegeEscalation: false` | Blocks privilege escalation inside the container. |
| `readOnlyRootFilesystem: true` | Forces writes into explicit hostPath mounts. |
| `capabilities.drop: [ALL]` | Runs without Linux capabilities. |
| immutable image digest | Prevents image drift during recovery. |
| liveness and readiness probes | Lets kubelet report provider process and dependency health. |

## Image Availability

For air-gapped or bootstrap-sensitive control planes, preload the image on every control-plane node:

- use immutable image digests,
- avoid `Always` pulls in recovery-sensitive deployments,
- keep the previous image available for rollback,
- document image import steps for node replacement.

`imagePullPolicy: IfNotPresent` is appropriate only when the exact digest has already been imported or is reliably pullable during node recovery. Do not rely on tag movement for upgrade or rollback.

## Host Preparation

Every control-plane node must have:

```text
/etc/openbao-kms/config.yaml
/etc/openbao-kms/tls/ca.crt
/var/lib/openbao-kms/identity.jwt
/etc/openbao-kms/client/client-chain.pem
/etc/openbao-kms/pkcs11/pin
/var/lib/openbao-kms/state
/run/openbao-kms
```

The JWT path is needed only for `auth.method: jwt`. Certificate auth deployments should instead mount the configured certificate chain and PKCS#11 PIN file, or the SPIFFE Workload API socket required by `auth.cert.spiffe.workloadAPISocket`.

The API server must be able to access the socket created under `/run/openbao-kms`. The container user must own the socket directory, or an equally narrow provider-only identity must be the only writer. The API server's socket access group needs execute permission on the directory and write permission on `kms.sock`; it must not have write permission on the directory itself.

The provider configuration used by the static pod sets `server.socketGroup` to the same numeric host GID listed in `supplementalGroups`. See `deploy/config/provider-static-pod.yaml` for the matching configuration sample.

Every provider in a multi-control-plane cluster must use the same identity-bearing configuration: provider name, cluster ID, OpenBao instance ID, Transit mount ID, key lineage ID, Transit mount path, and Transit key name. Multi-control-plane validation exercises this model with one node-local provider per API server.

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

The provider retries its initial status probe for `bootstrap.graceTimeout` before exiting. Static pod deployments should keep this enabled because auth material, container networking, DNS, OpenBao availability, and clock sync can settle after the container process starts.

For single-node control planes, systemd is usually safer. See [Deployment: Choosing A Model](/deployment/choosing-a-model/).

## Verification

Before enabling API server encryption:

1. Place the static pod manifest under `/etc/kubernetes/manifests/`.
2. Confirm the pod is running through kubelet or container runtime tooling.
3. Confirm `/run/openbao-kms/kms.sock` exists on the host.
4. Run `bao-kms-provider doctor` on the host or in an equivalent debug container.
5. Confirm `kube-apiserver` can connect to the socket.

After the Kubernetes `EncryptionConfiguration` is staged, include it in `doctor`:

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/encryption-config.yaml
```

## Source References

- [Kubernetes static Pods](https://kubernetes.io/docs/tasks/configure-pod-container/static-pod/)
- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
