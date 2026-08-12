# Linux Package Layout

These files are packaging inputs for Linux systemd hosts. The release workflow
uses `nfpm.yaml` to turn the built Linux binaries into `.deb` and `.rpm`
packages. The package installs the binary, systemd unit, sysusers metadata,
tmpfiles metadata, documentation, and examples.

Install release packages only after verifying the selected release's checksum,
signature, and provenance evidence.

Recommended install paths:

```text
/usr/bin/bao-kms-provider
/etc/openbao-kms/config.yaml
/etc/openbao-kms/tls/ca.crt
/var/lib/openbao-kms/identity.jwt
/etc/openbao-kms/client/client-chain.pem
/etc/openbao-kms/pkcs11/pin
/var/lib/openbao-kms/state/key-registry.json
/run/openbao-kms/kms.sock
```

The JSON Web Token (JWT), client certificate, and PKCS#11 personal
identification number (PIN) paths contain deployment-specific authentication
material. Mount or create only the paths used by the selected provider
authentication method.

Files under `/etc/openbao-kms/` are operator-owned. The package creates the
directory layout and installs examples under `/usr/share/doc/bao-kms-provider`;
it does not install, replace, or migrate the live provider configuration,
certificate authority (CA)
bundle, JWT, certificate chain, or PKCS#11 PIN files during upgrade.

Install package metadata:

- `sysusers.d/openbao-kms.conf` creates `openbao-kms` and `openbao-kms-socket`.
- `tmpfiles.d/openbao-kms.conf` creates the runtime, config, and state directories.
- `scripts/postinstall.sh` runs `systemd-sysusers`, `systemd-tmpfiles --create`,
  and `systemctl daemon-reload` when those tools are available.
- `../../systemd/bao-kms-provider.service` runs the provider as `openbao-kms`.

The package does not enable or start the service. Starting the provider changes
the control-plane boot path and requires an operator action.

Removing the package does not stop a running provider process or delete state
and configuration. During a maintenance window, stop the service before you
remove the package.

The socket group is separate from the provider primary group so kube-apiserver
can connect to the socket without receiving access to provider auth material. If
kube-apiserver runs as a non-root user, that user must be a member of
`openbao-kms-socket`; kubeadm static-pod API servers usually run as root and do
not need this group membership.
