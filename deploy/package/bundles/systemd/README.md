# systemd Release Bundle

This bundle is the tarball fallback for systemd hosts that do not consume
native `.deb` or `.rpm` packages. Verify the archive's checksum, signature,
and provenance before installation.

Run the following block as root from this extracted directory. It requires
GNU `install`, `systemd-sysusers`, and `systemd-tmpfiles`.

<!-- systemd-bundle-install -->
```sh
set -eu
install -D -o root -g root -m 0755 bin/bao-kms-provider /usr/bin/bao-kms-provider
install -D -o root -g root -m 0644 systemd/bao-kms-provider.service /usr/lib/systemd/system/bao-kms-provider.service
install -D -o root -g root -m 0644 sysusers.d/openbao-kms.conf /usr/lib/sysusers.d/openbao-kms.conf
install -D -o root -g root -m 0644 tmpfiles.d/openbao-kms.conf /usr/lib/tmpfiles.d/openbao-kms.conf
install -D -o root -g root -m 0644 config/provider-systemd.yaml /usr/share/doc/bao-kms-provider/examples/provider-systemd.yaml
install -D -o root -g root -m 0644 kubernetes/encryption-config.yaml /usr/share/doc/bao-kms-provider/examples/encryption-config.yaml
install -D -o root -g root -m 0644 README.md /usr/share/doc/bao-kms-provider/README.md
install -D -o root -g root -m 0644 LICENSE /usr/share/doc/bao-kms-provider/LICENSE
systemd-sysusers /usr/lib/sysusers.d/openbao-kms.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/openbao-kms.conf
```

Run `systemctl daemon-reload` to register the unit. The installation leaves
live configuration and authentication material unchanged. On a new host, it
does not enable or start the service.

Prepare `/etc/openbao-kms/config.yaml` from the installed example under
`/usr/share/doc/bao-kms-provider/examples/`. Provision the OpenBao CA bundle and
JWT, then validate as the service user:

```sh
sudo -u openbao-kms bao-kms-provider doctor --config /etc/openbao-kms/config.yaml
```

Continue only if `doctor` exits with status `0` without a `[fail]` check.
Follow [Install](https://dc-tec.github.io/openbao-kubernetes-kms/getting-started/install/)
for file ownership, artifact verification, and configuration instructions.
Follow [systemd Deployment](https://dc-tec.github.io/openbao-kubernetes-kms/deployment/systemd/)
to enable and start the service after validation.
