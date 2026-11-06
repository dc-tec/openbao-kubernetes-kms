---
title: Release notes and corrections
description: "The preview.3 installation boundary and historical preview.2 corrections."
weight: 1
---

## Preview.3 installation boundary

`0.1.0-preview.3` was published on 20 October 2026. Download matching artifacts
from the [release page](https://github.com/dc-tec/openbao-kubernetes-kms/releases/tag/0.1.0-preview.3)
and follow the [preview.3 installation guide](/docs/get-started/).

Preview.3 requires a fresh disposable installation with a new Transit key and
provider identity. An earlier preview cannot be upgraded in place. Preserve
its registry, checkpoint, key material, and matching artifacts.

## Historical preview.2 corrections

All remaining sections on this page apply only to `0.1.0-preview.2`. They
preserve the published correction anchors and link to immutable corrected
preview.2 guides. They do not describe preview.3 installation behavior.
Download preview.2 assets only from its
[release page](https://github.com/dc-tec/openbao-kubernetes-kms/releases/tag/0.1.0-preview.2).
Corrections to instructions do not change those released binaries.

## Available commands and authentication

Preview.2 has no `init` command or native OAuth client-credentials source.
Use the manual configuration in these guides and an independently renewed
host JWT file. Do not add `auth.jwt.source` to this release's configuration.
Preview.3 includes both features for fresh installations; follow its
[released installation guide](/docs/get-started/).

## systemd directory permissions

The published packages and tarballs create `/etc/openbao-kms` with group
`root` and mode `0750`. The `openbao-kms` user cannot traverse that directory.
Apply the persistent tmpfiles override in the
[systemd guide](https://github.com/dc-tec/openbao-kubernetes-kms/blob/585c77cf259d975a74c24061d5f6cae0dbeafedf/website/content-versions/0.1.x/docs/deployment/systemd.md#directory-setup) before starting the
service. The override also prevents a later tmpfiles run from restoring the
incorrect group. Preserve any existing operator override.

## systemd tarball layout

The systemd tarball contains a versioned directory with `bin/`, `systemd/`,
`sysusers.d/`, and `tmpfiles.d/`. Extract it into a working directory and follow
the [installation commands](https://github.com/dc-tec/openbao-kubernetes-kms/blob/585c77cf259d975a74c24061d5f6cae0dbeafedf/website/content-versions/0.1.x/docs/getting-started/install.md#install-the-systemd-tarball).
Do not extract it directly over the root filesystem.

## Static-pod diagnostic binary

The static-pod bundle does not contain a host diagnostic binary. Obtain the
same-version systemd tarball for the host architecture and extract its
`bin/bao-kms-provider` for diagnostics. Do not install its systemd unit or
host ownership settings for a static-pod deployment.

## Fresh evaluation clusters

The installation procedure targets fresh, disposable evaluation clusters.
If an API server already uses an `EncryptionConfiguration`, stop: these guides
do not import or replace it. Migration from another provider and retirement of
old encryption readers are deferred until stable-release planning.

Even a fresh cluster can have several API servers. Configure the new KMS reader
on every API server before enabling KMS writes. Keep `identity` available for
plaintext objects in this preview procedure. Follow
[Kubernetes encryption configuration](https://github.com/dc-tec/openbao-kubernetes-kms/blob/585c77cf259d975a74c24061d5f6cae0dbeafedf/website/content-versions/0.1.x/docs/getting-started/kubernetes-encryption-config.md).

## Diagnostic scope

In this release, `doctor` creates a local diagnostic KMS server. It does not
probe the running provider socket or prove API-server integration. A check run
as root also does not establish that the service or container user can read
its files. Validate the actual runtime and complete
[First encrypt](https://github.com/dc-tec/openbao-kubernetes-kms/blob/585c77cf259d975a74c24061d5f6cae0dbeafedf/website/content-versions/0.1.x/docs/getting-started/first-encrypt.md) on every API server.
