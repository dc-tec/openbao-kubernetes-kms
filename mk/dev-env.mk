##@ Dev Environment

DEV_ENV_DIR ?= test/dev-env
DEV_ENV_KIND_CLUSTER ?= openbao-kms-dev
AUTH ?= jwt
DEV_ENV_AUTH ?= $(AUTH)
DEV_ENV_PROVIDER_IMAGE ?= $(if $(filter pkcs11,$(DEV_ENV_AUTH)),$(IMAGE_REPOSITORY):dev-env-pkcs11,$(IMAGE_REPOSITORY):dev-env)
DEV_ENV_OPENBAO_IMAGE ?= $(E2E_OPENBAO_IMAGE)
DEV_ENV_PROMETHEUS_IMAGE ?= docker.io/prom/prometheus:v2.55.1
DEV_ENV_GRAFANA_IMAGE ?= docker.io/grafana/grafana:11.3.0
DEV_ENV_COMPOSE_PROJECT ?= openbao-kms-dev-env
DEV_ENV_COMPOSE = DEV_ENV_OPENBAO_IMAGE="$(DEV_ENV_OPENBAO_IMAGE)" \
	DEV_ENV_PROMETHEUS_IMAGE="$(DEV_ENV_PROMETHEUS_IMAGE)" \
	DEV_ENV_GRAFANA_IMAGE="$(DEV_ENV_GRAFANA_IMAGE)" \
	docker compose -f "$(DEV_ENV_DIR)/compose.yaml" --project-name "$(DEV_ENV_COMPOSE_PROJECT)"
DEV_ENV_SCRIPT_ENV = DEV_ENV_KIND_CLUSTER="$(DEV_ENV_KIND_CLUSTER)" \
	DEV_ENV_PROVIDER_IMAGE="$(DEV_ENV_PROVIDER_IMAGE)" \
	DEV_ENV_AUTH="$(DEV_ENV_AUTH)" \
	DEV_ENV_COMPOSE_PROJECT="$(DEV_ENV_COMPOSE_PROJECT)"

.PHONY: dev-env-up
dev-env-up: dev-env-generate dev-env-kind dev-env-compose-up dev-env-openbao dev-env-build dev-env-pkcs11-material dev-env-tofu dev-env-stage dev-env-enable-kms dev-env-verify ## Create the local Kind/OpenBao KMS dev environment.

.PHONY: dev-env-generate
dev-env-generate: ## Generate local ignored dev-env identity material.
	@$(DEV_ENV_SCRIPT_ENV) "$(DEV_ENV_DIR)/scripts/generate-material.sh"

.PHONY: dev-env-kind
dev-env-kind: ## Create the Kind cluster used by the dev environment.
	@$(DEV_ENV_SCRIPT_ENV) "$(DEV_ENV_DIR)/scripts/create-kind.sh"

.PHONY: dev-env-compose-up
dev-env-compose-up: ## Start OpenBao, Prometheus, and Grafana for the dev environment.
	@$(DEV_ENV_COMPOSE) up -d

.PHONY: dev-env-openbao
dev-env-openbao: ## Initialize, unseal, and wait for local OpenBao readiness.
	@$(DEV_ENV_SCRIPT_ENV) "$(DEV_ENV_DIR)/scripts/init-openbao.sh"

.PHONY: dev-env-tofu
dev-env-tofu: ## Apply OpenBao dev-env configuration with OpenTofu.
	@$(DEV_ENV_SCRIPT_ENV) "$(DEV_ENV_DIR)/scripts/tofu-apply.sh"

.PHONY: dev-env-build
dev-env-build: ## Build and load the provider image into Kind.
	@if [ "$(DEV_ENV_AUTH)" = "pkcs11" ]; then \
		$(MAKE) image-certauth-pkcs11-e2e E2E_PROVIDER_CERTAUTH_PKCS11_IMAGE="$(DEV_ENV_PROVIDER_IMAGE)"; \
	else \
		$(MAKE) image IMAGE="$(DEV_ENV_PROVIDER_IMAGE)"; \
	fi
	@kind load docker-image --name "$(DEV_ENV_KIND_CLUSTER)" "$(DEV_ENV_PROVIDER_IMAGE)"

.PHONY: dev-env-pkcs11-material
dev-env-pkcs11-material: ## Generate SoftHSM PKCS#11 material when AUTH=pkcs11.
	@$(DEV_ENV_SCRIPT_ENV) "$(DEV_ENV_DIR)/scripts/setup-softhsm.sh"

.PHONY: dev-env-stage
dev-env-stage: ## Stage provider files and static pod manifest into the Kind control-plane node.
	@$(DEV_ENV_SCRIPT_ENV) "$(DEV_ENV_DIR)/scripts/stage-provider.sh"

.PHONY: dev-env-enable-kms
dev-env-enable-kms: ## Enable kube-apiserver KMS encryption in the Kind control-plane node.
	@$(DEV_ENV_SCRIPT_ENV) "$(DEV_ENV_DIR)/scripts/enable-kms.sh"

.PHONY: dev-env-verify
dev-env-verify: ## Verify upstream Kubernetes KMS v2 encryption and raw etcd envelope storage.
	@$(DEV_ENV_SCRIPT_ENV) "$(DEV_ENV_DIR)/scripts/verify-kms.sh"

.PHONY: dev-env-down
dev-env-down: ## Stop Compose services and delete the Kind cluster, keeping generated state.
	@$(DEV_ENV_COMPOSE) down --remove-orphans
	@kind delete cluster --name "$(DEV_ENV_KIND_CLUSTER)" >/dev/null 2>&1 || true

.PHONY: dev-env-reset
dev-env-reset: dev-env-down ## Delete the dev environment and generated local state.
	@rm -rf "$(DEV_ENV_DIR)/.state"

.PHONY: dev-env-logs
dev-env-logs: ## Tail dev environment Compose logs.
	@$(DEV_ENV_COMPOSE) logs -f

.PHONY: dev-env-grafana
dev-env-grafana: ## Print the local Grafana URL for the dev environment.
	@printf '%s\n' 'Grafana: http://127.0.0.1:18300  user: admin  password: admin'
