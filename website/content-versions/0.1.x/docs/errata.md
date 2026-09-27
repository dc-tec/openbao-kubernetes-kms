---
title: Release notes and corrections
description: "Installation corrections and feature boundaries for 0.1.0-preview.2."
weight: 1
---

These docs start from the `0.1.0-preview.2` source tag and include the
installation corrections listed here. Download the matching assets from the
[release page](https://github.com/dc-tec/openbao-kubernetes-kms/releases/tag/0.1.0-preview.2).
Changes to these instructions do not change the released binaries.

## Available commands and authentication

Preview.2 has no `init` command or native OAuth client-credentials source.
Use the manual configuration in these guides and an independently renewed
host JWT file. Do not add `auth.jwt.source` to this release's configuration.
The current [development docs](/next/docs/) describe the later implementation.

## systemd directory permissions

The published packages and tarballs create `/etc/openbao-kms` with group
`root` and mode `0750`. The `openbao-kms` user cannot traverse that directory.
Apply the persistent tmpfiles override in the
[systemd guide](/docs/deployment/systemd/#directory-setup) before starting the
service. The override also prevents a later tmpfiles run from restoring the
incorrect group. Preserve any existing operator override.

## systemd tarball layout

The systemd tarball contains a versioned directory with `bin/`, `systemd/`,
`sysusers.d/`, and `tmpfiles.d/`. Extract it into a working directory and follow
the [installation commands](/docs/getting-started/install/#install-the-systemd-tarball).
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
[Kubernetes encryption configuration](/docs/getting-started/kubernetes-encryption-config/).

## Diagnostic scope

In this release, `doctor` creates a local diagnostic KMS server. It does not
probe the running provider socket or prove API-server integration. A check run
as root also does not establish that the service or container user can read
its files. Validate the actual runtime and complete
[First encrypt](/docs/getting-started/first-encrypt/) on every API server.
