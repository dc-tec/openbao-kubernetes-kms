##@ Deployment

.PHONY: deployment-samples-check
deployment-samples-check: ## Validate deployment sample manifests and scripts.
	@"$(GO)" test ./test/deployment
	@for script in hack/kubeadm/*.sh; do sh -n "$$script"; done
	@for script in hack/harvester/*.sh hack/harvester/remote/*.sh; do sh -n "$$script"; done
	@for script in deploy/package/linux/scripts/*.sh; do sh -n "$$script"; done
	@if command -v systemd-analyze >/dev/null 2>&1; then \
		systemd-analyze verify deploy/systemd/bao-kms-provider.service; \
	else \
		printf '%s\n' 'systemd-analyze not installed; skipping systemd unit verification.'; \
	fi
