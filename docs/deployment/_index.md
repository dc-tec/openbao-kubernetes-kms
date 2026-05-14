---
title: "Deployment"
description: "Choose between systemd and static-pod, then apply the matching Linux identity, file paths, and runtime hardening."
weight: 20
browse:
  - "/deployment/choosing-a-model"
  - "/deployment/systemd"
  - "/deployment/static-pod"
  - "/deployment/linux-identity-model"
  - "/deployment/observability"
---

# Deployment

Use this section to select a deployment model and apply the matching system
identity, file paths, and runtime hardening. The tested preview deployment
models are systemd and static pod.

## Pick A Model First

1. [Choosing A Model](/deployment/choosing-a-model/) to compare systemd and static-pod against your control-plane topology, kubeadm posture, and operational constraints.
2. [systemd Deployment](/deployment/systemd/) for a hardened systemd unit on the control-plane host.
3. [Static Pod Deployment](/deployment/static-pod/) for a kubelet-managed static pod alongside the API server.
4. [Linux Identity Model](/deployment/linux-identity-model/) for the user, group, file ownership, and permission model that both deployment styles depend on.
5. [Observability Deployment](/deployment/observability/) for Prometheus scrape wiring and the maintained Grafana dashboard sample.

## Use Another Section If

- the question is about getting a binary onto the host or wiring `EncryptionConfiguration`: go to [Start Here](/getting-started/).
- the question is about ongoing operation, rotation, or recovery: go to [Operations](/operations/).
- the question is about runtime hardening and trust boundaries beyond the host identity: go to [Security](/security/).
