##@ Deployment

.PHONY: deployment-samples-check
deployment-samples-check: ## Validate deployment sample manifests and scripts.
	@"$(GO)" test ./test/deployment
	@for script in hack/kubeadm/*.sh; do sh -n "$$script"; done
	@for script in hack/harvester/*.sh hack/harvester/remote/*.sh; do sh -n "$$script"; done
	@for script in deploy/package/linux/scripts/*.sh; do sh -n "$$script"; done
	@for script in hack/install/*.sh; do sh -n "$$script"; done
	@for script in test/deployment/*.sh; do bash -n "$$script"; done
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

.PHONY: systemd-install-check
systemd-install-check: ## Exercise the documented tarball installation in a disposable Linux container.
	@set -eu; \
	builder="$$(awk '/^  imageBuilderBase:/{print $$2}' .ci/versions.yaml)"; \
	digest="$$(awk '/^  imageBuilderBaseDigest:/{print $$2}' .ci/versions.yaml)"; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	docker build --platform "$(IMAGE_PLATFORM)" --iidfile "$$tmp/image-id" \
		--build-arg "BUILDER_IMAGE=$$builder@$$digest" \
		-f test/deployment/Dockerfile.systemd-install test/deployment; \
	docker run --rm --platform "$(IMAGE_PLATFORM)" --network=none --user 0:0 \
		--mount "type=bind,source=$(CURDIR),target=/src,readonly" \
		--env KMS_INSTALL_TEST_CONTAINER=1 --env "BUNDLE_ARCHIVE=$(BUNDLE_ARCHIVE)" \
		--workdir /src \
		"$$(cat "$$tmp/image-id")" bash test/deployment/systemd-install.sh

.PHONY: static-pod-install-check
static-pod-install-check: ## Exercise the static-pod kit without activating a provider or contacting OpenBao.
	@set -eu; \
	builder="$$(awk '/^  imageBuilderBase:/{print $$2}' .ci/versions.yaml)"; \
	digest="$$(awk '/^  imageBuilderBaseDigest:/{print $$2}' .ci/versions.yaml)"; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	docker build --platform "$(IMAGE_PLATFORM)" --iidfile "$$tmp/image-id" \
		--build-arg "BUILDER_IMAGE=$$builder@$$digest" \
		-f test/deployment/Dockerfile.systemd-install test/deployment; \
	docker run --rm --platform "$(IMAGE_PLATFORM)" --network=none --user 0:0 \
		--mount "type=bind,source=$(CURDIR),target=/src,readonly" \
		--env KMS_INSTALL_TEST_CONTAINER=1 --env "BUNDLE_ARCHIVE=$(BUNDLE_ARCHIVE)" \
		--workdir /src \
		"$$(cat "$$tmp/image-id")" bash test/deployment/static-pod-install.sh

.PHONY: native-package-install-check
native-package-install-check: ## Install an exact deb/rpm artifact, check access and preservation, then remove it.
	@set -eu; \
	test -n "$(PACKAGE_FILE)"; test -n "$(PACKAGE_VERSION)"; \
	case "$(PACKAGE_FILE)" in \
	  *.deb) base_key=imageBuilderBase; dockerfile=test/deployment/Dockerfile.systemd-install; arg=BUILDER_IMAGE ;; \
	  *.rpm) base_key=packageRPMTestBase; dockerfile=test/deployment/Dockerfile.rpm-install; arg=RPM_IMAGE ;; \
	  *) printf '%s\n' 'PACKAGE_FILE must be a .deb or .rpm'; exit 2 ;; \
	esac; \
	base="$$(awk -v key="$$base_key:" '$$1 == key {print $$2}' .ci/versions.yaml)"; \
	digest="$$(awk -v key="$${base_key}Digest:" '$$1 == key {print $$2}' .ci/versions.yaml)"; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	docker build --platform "$(IMAGE_PLATFORM)" --iidfile "$$tmp/image-id" \
		--build-arg "$$arg=$$base@$$digest" -f "$$dockerfile" test/deployment; \
	docker run --rm --platform "$(IMAGE_PLATFORM)" --network=none --user 0:0 \
		--mount "type=bind,source=$(CURDIR),target=/src,readonly" \
		--env KMS_INSTALL_TEST_CONTAINER=1 --env "PACKAGE_FILE=$(PACKAGE_FILE)" \
		--env "PACKAGE_ARCH=$(patsubst linux/%,%,$(IMAGE_PLATFORM))" --env "PACKAGE_VERSION=$(PACKAGE_VERSION)" \
		--workdir /src "$$(cat "$$tmp/image-id")" bash test/deployment/native-package-install.sh
