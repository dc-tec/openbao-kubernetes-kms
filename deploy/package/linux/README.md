# Linux Package Layout

These files are packaging inputs for Linux systemd hosts. They are not a distro package by themselves.

Recommended install paths:

```text
/usr/local/bin/bao-kms-provider
/etc/openbao-kms/config.yaml
/etc/openbao-kms/tls/ca.crt
/var/lib/openbao-kms/identity.jwt
/var/lib/openbao-kms/state/key-registry.json
/run/openbao-kms/kms.sock
```

Install package metadata:

- `sysusers.d/openbao-kms.conf` creates `openbao-kms` and `openbao-kms-socket`.
- `tmpfiles.d/openbao-kms.conf` creates the runtime, config, and state directories.
- `../../systemd/bao-kms-provider.service` runs the provider as `openbao-kms`.

The socket group is separate from the provider primary group so kube-apiserver can connect to the socket without receiving access to the provider JWT.
