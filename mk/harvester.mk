##@ Harvester Lab

HARVESTER_LAB_ACTION ?= $(ACTION)
HARVESTER_LAB_ACTIONS := \
	values \
	lint \
	render \
	dry-run \
	create \
	status \
	wait \
	ssh-config \
	wait-ssh \
	bootstrap-openbao \
	bootstrap-kubeadm \
	bootstrap-mcp \
	bootstrap-guests \
	verify-guests \
	wire-provider \
	wire-systemd \
	wire-static \
	wire-mcp \
	verify-kms \
	verify-recovery \
	verify-openbao-outage \
	verify-load \
	verify-decrypt-warmup \
	verify-decrypt-cold-start \
	verify-upgrade-rollback \
	verify-paired-restore \
	verify-mcp-recovery \
	production-gate \
	e2e \
	destroy

.PHONY: harvester-lab
harvester-lab: ## Run a local Harvester lab action, e.g. ACTION=status.
	@if [ -z "$(HARVESTER_LAB_ACTION)" ]; then \
		printf '%s\n' 'Set HARVESTER_LAB_ACTION, for example: make harvester-lab HARVESTER_LAB_ACTION=status'; \
		printf '%s\n' 'Available actions: $(HARVESTER_LAB_ACTIONS)'; \
		exit 2; \
	fi
	@hack/harvester/lab.sh "$(HARVESTER_LAB_ACTION)"

.PHONY: $(addprefix harvester-lab-,$(HARVESTER_LAB_ACTIONS))
$(addprefix harvester-lab-,$(HARVESTER_LAB_ACTIONS)): harvester-lab-%:
	@hack/harvester/lab.sh "$*"
