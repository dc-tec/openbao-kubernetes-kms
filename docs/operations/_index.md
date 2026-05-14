---
title: "Operations"
description: "Task-focused operator guidance for rotation, disaster recovery, upgrade, and troubleshooting."
weight: 30
browse:
  - "/operations/rotation"
  - "/operations/disaster-recovery"
  - "/operations/upgrade"
  - "/operations/troubleshooting"
---

# Operations

These pages answer task-based operator questions. Use them when you already know where you are in the lifecycle and need the next safe step.

The runbooks assume `bao-kms-provider` is already installed on each control-plane node, OpenBao Transit is provisioned, and the Kubernetes API server uses a matching `EncryptionConfiguration`. Before changing rotation, recovery, or upgrade state, run `bao-kms-provider doctor --config /etc/openbao-kms/config.yaml --encryption-config /etc/kubernetes/encryption-config.yaml`.

## Workflows

1. [Rotation](/operations/rotation/) to rotate the OpenBao Transit key version, observe provider promotion, migrate Kubernetes resources, and keep old versions decryptable until migration and backup-retention records allow retirement.
2. [Disaster Recovery](/operations/disaster-recovery/) to restore OpenBao, etcd, provider state, auth material, and control-plane nodes as compatible sets.
3. [Upgrade](/operations/upgrade/) to upgrade the provider binary or container image one control-plane node at a time with a documented rollback step.
4. [Troubleshooting](/operations/troubleshooting/) for symptom-driven checks and the fastest safe recovery path.

## Use Another Section If

- the question is about CLI flags, configuration fields, or KMS v2 protocol behavior: go to [Reference](/reference/).
- the question is about token scope, trust boundaries, or sensitive artifact handling: go to [Security](/security/).
- the question is about why the system behaves a given way: go to [Architecture](/architecture/).
