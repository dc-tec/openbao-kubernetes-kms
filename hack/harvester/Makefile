SHELL := /bin/sh

ACTION ?=
LAB := ./lab.sh

LAB_ACTIONS := \
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

.PHONY: help
help:
	@printf '%s\n' 'Usage:'
	@printf '%s\n' '  make -C hack/harvester <action>'
	@printf '%s\n' ''
	@printf '%s\n' 'Actions:'
	@for action in $(LAB_ACTIONS); do printf '  %s\n' "$$action"; done

.PHONY: lab
lab:
	@if [ -z "$(ACTION)" ]; then \
		printf '%s\n' 'Set ACTION, for example: make -C hack/harvester lab ACTION=status'; \
		exit 2; \
	fi
	@$(LAB) "$(ACTION)"

.PHONY: $(LAB_ACTIONS)
$(LAB_ACTIONS):
	@$(LAB) "$@"
