---
title: Documentation
description: Install, deploy, operate, secure, and understand the OpenBao Kubernetes KMS provider.
eyebrow: Provider handbook
weight: 1
hideChildren: true
---

Use the route that matches the work in front of you. This manual describes the
preview 0.1.x release line.

<div class="link-grid">
  <a href="getting-started/"><strong>Start Here</strong><p>Set up OpenBao Transit, install the provider, wire Kubernetes encryption, and verify it end to end.</p></a>
  <a href="deployment/"><strong>Deployment</strong><p>Choose between systemd and static pod, then apply the Linux identity, file layout, and hardening.</p></a>
  <a href="operations/"><strong>Operations</strong><p>Rotate keys, upgrade, recover from disasters, and troubleshoot a running provider.</p></a>
  <a href="security/"><strong>Security</strong><p>Review the threat model, hardening requirements, authentication, and decrypt validation.</p></a>
  <a href="reference/"><strong>Reference</strong><p>Check the CLI, configuration, KMS v2 contract, metrics, compatibility, and release policy.</p></a>
  <a href="architecture/"><strong>Architecture</strong><p>Understand the key model, rotation model, failure modes, and design rationale.</p></a>
</div>

{{< callout type="warning" title="Preview release" >}}
The provider is a preview release. Use it for labs, staging, and evaluation.
Do not protect production control planes with a preview release.
{{< /callout >}}
