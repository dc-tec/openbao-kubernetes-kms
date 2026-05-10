#!/bin/sh
set -eu

ROOT="${ROOT:-}"
BINARY="${BINARY:-./bin/bao-kms-provider}"
CONFIG="${CONFIG:-./deploy/config/provider-systemd.yaml}"
SERVICE_FILE="${SERVICE_FILE:-./deploy/systemd/bao-kms-provider.service}"
SYSUSERS_FILE="${SYSUSERS_FILE:-./deploy/package/linux/sysusers.d/openbao-kms.conf}"
TMPFILES_FILE="${TMPFILES_FILE:-./deploy/package/linux/tmpfiles.d/openbao-kms.conf}"

require_root() {
	if [ "$(id -u)" -ne 0 ]; then
		printf '%s\n' "run as root or set ROOT to a staging directory" >&2
		exit 1
	fi
}

install_file() {
	src="$1"
	dst="$2"
	mode="$3"
	owner="$4"
	group="$5"
	if [ -n "$ROOT" ] && [ "${PRESERVE_OWNERS:-false}" != "true" ]; then
		install -D -m "$mode" "$src" "${ROOT}${dst}"
	else
		install -D -m "$mode" -o "$owner" -g "$group" "$src" "${ROOT}${dst}"
	fi
}

if [ -z "$ROOT" ]; then
	require_root
fi

install_file "$BINARY" /usr/bin/bao-kms-provider 0755 root root
install_file "$CONFIG" /etc/openbao-kms/config.yaml 0640 root openbao-kms
install_file "$SERVICE_FILE" /etc/systemd/system/bao-kms-provider.service 0644 root root
install_file "$SYSUSERS_FILE" /usr/lib/sysusers.d/openbao-kms.conf 0644 root root
install_file "$TMPFILES_FILE" /usr/lib/tmpfiles.d/openbao-kms.conf 0644 root root

if [ -z "$ROOT" ]; then
	systemd-sysusers /usr/lib/sysusers.d/openbao-kms.conf
	systemd-tmpfiles --create /usr/lib/tmpfiles.d/openbao-kms.conf
	systemctl daemon-reload
	printf '%s\n' "installed systemd lab files; run bao-kms-provider doctor before starting the service"
else
	printf '%s\n' "staged systemd lab files under ${ROOT}"
fi
