#!/bin/sh
set -eu

warn() {
	printf '%s\n' "warning: $*" >&2
}

if command -v systemd-sysusers >/dev/null 2>&1; then
	systemd-sysusers /usr/lib/sysusers.d/openbao-kms.conf
else
	warn "systemd-sysusers not found; create openbao-kms users and groups manually"
fi

if command -v getent >/dev/null 2>&1; then
	if ! getent passwd openbao-kms >/dev/null 2>&1; then
		warn "openbao-kms user is missing; run systemd-sysusers /usr/lib/sysusers.d/openbao-kms.conf"
	fi
	if ! getent group openbao-kms-socket >/dev/null 2>&1; then
		warn "openbao-kms-socket group is missing; run systemd-sysusers /usr/lib/sysusers.d/openbao-kms.conf"
	fi
else
	warn "getent not found; cannot verify openbao-kms users and groups"
fi

if command -v systemd-tmpfiles >/dev/null 2>&1; then
	systemd-tmpfiles --create /usr/lib/tmpfiles.d/openbao-kms.conf
else
	warn "systemd-tmpfiles not found; create openbao-kms directories manually"
fi

if [ ! -d /run/openbao-kms ]; then
	warn "/run/openbao-kms was not created; run systemd-tmpfiles --create /usr/lib/tmpfiles.d/openbao-kms.conf"
fi

if command -v systemctl >/dev/null 2>&1; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi
