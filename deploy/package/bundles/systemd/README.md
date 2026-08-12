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

Before installation, verify the package or tarball against the selected
release's checksums, signature, and provenance evidence. Then complete these
steps on every control-plane host:

1. Install the binary and metadata.
2. Run `systemd-sysusers`.
3. Run `systemd-tmpfiles --create`.
4. Run `bao-kms-provider doctor --config /etc/openbao-kms/config.yaml`.

Continue only if `doctor` exits with status `0`. You can then enable or start
`bao-kms-provider.service`.

The package and bundle do not enable or start the service. Starting the provider
changes the control-plane boot path and requires an operator action.
