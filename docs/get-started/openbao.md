---
title: Prepare OpenBao
description: "Review and run the generated setup that creates the Transit mount and key, the least-privilege policy, and the JWT auth role."
eyebrow: Get started · Step 4
weight: 40
verifiedBy:
  - cmd/bao-kms-provider/init.go
  - cmd/bao-kms-provider/policy.go
---

Run these steps once per Kubernetes cluster as an OpenBao administrator. At the
end, OpenBao holds a Transit key the provider can use for encrypt and decrypt
only, and a JWT role that issues the provider a token with exactly that
permission.

## Before you begin

- Generate and review the files from [Generate installation files](/docs/get-started/plan-values/).
- Use an OpenBao endpoint with valid TLS that serves requests without HTTP
  redirects. For HA, use server-side request forwarding or an endpoint routed
  to the active node.
- Log in to the `bao` CLI with a token that can manage `sys/mounts`, `sys/auth`,
  `sys/policies`, and the new Transit mount.
- Confirm that the JWT issuer for the provider host credential is reachable
  independently of the protected Kubernetes API server. See
  [Security: Auth model](/docs/security/auth-model/).

## Run the generated setup

Review `generated/openbao-setup.sh` and `generated/openbao-policy.hcl`.
Set the OpenBao address and CA bundle for the administrator's CLI, then run the
script once for the new Transit key:

```sh
export BAO_ADDR=https://bao.example.internal:8200
export BAO_CACERT=/path/to/openbao-ca.crt
sh generated/openbao-setup.sh
```

Use the address and trust bundle for your OpenBao instance. The script applies
a configured namespace itself. It expects new dedicated mounts and is not a
reconciliation loop. Investigate any failure before rerunning commands.

The script accepts one phase: `transit`, `policy`, `auth-mount`, `auth-config`,
or `auth-role`. With no argument it runs all phases in that order. After a
partial failure, inspect the completed resources against the generated commands
and your recorded identity before running the remaining phases. A selected
phase does not verify existing resources or adopt a different deployment.

For example, if OIDC discovery failed after the JWT mount was enabled, correct
the issuer connectivity, confirm that the mount is the intended JWT mount, and
run:

```sh
sh generated/openbao-setup.sh auth-config
sh generated/openbao-setup.sh auth-role
```

Do not rerun `transit` or generate another lineage ID to recover an auth setup
failure. If creation stopped within the Transit phase, inspect the mount and
key and complete only the missing reviewed commands by hand.

`init --new-key` already recorded the key lineage ID in `generated/config.yaml`.
Do not generate another one.

To create the same resources by hand, through configuration management, or with
the OpenTofu module, see
[Configure: OpenBao auth and policy](/docs/configure/openbao-auth/#manual-setup).

## Result

OpenBao now has the Transit mount with `disable_upsert`, the key with the
recommended profile, the policy, and the JWT role. The provider checks this
setup with `verify-key` and `doctor` once it is installed on a node.

Continue with [Run with systemd](/docs/get-started/systemd/) or
[Run as a static pod](/docs/get-started/static-pod/).
