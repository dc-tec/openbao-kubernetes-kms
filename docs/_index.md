---
title: Documentation
description: Install, deploy, operate, secure, and understand the OpenBao Kubernetes KMS provider.
eyebrow: Provider handbook
weight: 1
hideChildren: true
---

Use the route that matches your task. These Next docs describe unreleased
behavior on `main`. Select the published release line when installing release
artifacts.

<div class="link-grid">
  <a href="get-started/"><strong>Get started</strong><p>Choose a deployment model, prepare OpenBao, install the provider, and enable encryption.</p></a>
  <a href="configure/"><strong>Configure</strong><p>Set up OpenBao authentication and policy variants, and monitor the provider.</p></a>
  <a href="operate/"><strong>Operate</strong><p>Rotate keys, upgrade, recover from disasters, and troubleshoot a running provider.</p></a>
  <a href="security/"><strong>Security</strong><p>Review the threat model, hardening, authentication, host identity, and decrypt validation.</p></a>
  <a href="reference/"><strong>Reference</strong><p>Check the CLI, configuration, KMS v2 contract, metrics, compatibility, and release policy.</p></a>
  <a href="architecture/"><strong>Architecture</strong><p>Understand the key model, rotation model, failure modes, and design rationale.</p></a>
</div>

{{< callout type="warning" title="Preview release" >}}
The provider is a preview release. Use it for labs, staging, and evaluation.
Do not protect production control planes with a preview release.
{{< /callout >}}
