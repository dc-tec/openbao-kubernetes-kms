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

## Workflows

1. [Rotation](/operations/rotation/) to rotate the Transit key version on the OpenBao side and observe the provider switch over without downtime.
2. [Disaster Recovery](/operations/disaster-recovery/) to handle OpenBao loss, Transit key loss, etcd restore, and emergency recovery paths.
3. [Upgrade](/operations/upgrade/) to upgrade the provider binary or container image with a documented rollback step.
4. [Troubleshooting](/operations/troubleshooting/) for common failure modes and the fastest safe recovery path.

## Use Another Section If

- the question is about CLI flags, configuration fields, or KMS v2 protocol behavior: go to [Reference](/reference/).
- the question is about token scope, trust boundaries, or sensitive artifact handling: go to [Security](/security/).
- the question is about why the system behaves a given way: go to [Architecture](/architecture/).
