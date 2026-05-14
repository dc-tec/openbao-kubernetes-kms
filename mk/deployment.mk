##@ Deployment

.PHONY: deployment-samples-check
deployment-samples-check: ## Validate deployment sample manifests and scripts.
	@"$(GO)" test ./test/deployment
	@for script in hack/kubeadm/*.sh; do sh -n "$$script"; done
	@for script in hack/harvester/*.sh hack/harvester/remote/*.sh; do sh -n "$$script"; done
	@for script in deploy/package/linux/scripts/*.sh; do sh -n "$$script"; done
	@if command -v systemd-analyze >/dev/null 2>&1; then \
		tmp="$$(mktemp -d)"; \
		trap 'rm -rf "$$tmp"' EXIT; \
		install -m 0755 /dev/null "$$tmp/bao-kms-provider"; \
		sed "s#/usr/bin/bao-kms-provider#$$tmp/bao-kms-provider#g" deploy/systemd/bao-kms-provider.service > "$$tmp/bao-kms-provider.service"; \
		systemd-analyze verify "$$tmp/bao-kms-provider.service"; \
	else \
		printf '%s\n' 'systemd-analyze not installed; skipping systemd unit verification.'; \
	fi

.PHONY: package-build-check
package-build-check: ## Build throwaway native packages to validate nFPM metadata.
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	binary="$$tmp/bao-kms-provider"; \
	printf '%s\n' '#!/bin/sh' 'exit 0' > "$$binary"; \
	chmod 0755 "$$binary"; \
	arch="$$("$(GO)" env GOARCH)"; \
	for format in $(PACKAGE_FORMATS); do \
		target="$$tmp/bao-kms-provider_0.0.0-package-check_linux_$${arch}.$$format"; \
		printf 'checking nFPM %s package metadata\n' "$$format"; \
		SOURCE_DATE_EPOCH=0 \
		VERSION=0.0.0-package-check \
		NFPM_ARCH="$$arch" \
		NFPM_RELEASE=1 \
		PACKAGE_BINARY="$$binary" \
			$(NFPM_RUN) package --config "$(NFPM_CONFIG)" --packager "$$format" --target "$$target" >/dev/null; \
		test -s "$$target"; \
	done
