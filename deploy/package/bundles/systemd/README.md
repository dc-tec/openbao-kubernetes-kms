# systemd Release Bundle

This bundle is the tarball fallback for hosts that do not consume native
`.deb` or `.rpm` packages.

It contains:

- `bin/bao-kms-provider`
- `systemd/bao-kms-provider.service`
- `sysusers.d/openbao-kms.conf`
- `tmpfiles.d/openbao-kms.conf`
- `config/provider-systemd.yaml`
- `kubernetes/encryption-config.yaml`

Install the binary and metadata on every control-plane host, run
`systemd-sysusers`, run `systemd-tmpfiles --create`, and then run
`bao-kms-provider doctor --config /etc/openbao-kms/config.yaml` before enabling
or starting `bao-kms-provider.service`.

The package and bundle do not auto-enable or auto-start the service. Starting
the provider changes the control-plane boot path and must be an explicit
operator action.
