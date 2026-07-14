#!/bin/sh
set -eu

OPENBAO_VERSION="${OPENBAO_VERSION:-2.6.0}"
OPENBAO_ARCH="${OPENBAO_ARCH:-x86_64}"
OPENBAO_IP="${OPENBAO_IP:?OPENBAO_IP is required}"
OPENBAO_TLS_SERVER_NAME="${OPENBAO_TLS_SERVER_NAME:-obk-openbao-1}"
OPENBAO_LAB_DIR="${OPENBAO_LAB_DIR:-/root/openbao-kms-lab}"

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y ca-certificates curl jq openssl tar

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

artifact="bao_${OPENBAO_VERSION}_Linux_${OPENBAO_ARCH}.tar.gz"
base_url="https://github.com/openbao/openbao/releases/download/v${OPENBAO_VERSION}"
curl -fsSLo "$tmp/$artifact" "$base_url/$artifact"
curl -fsSLo "$tmp/checksums-linux.txt" "$base_url/checksums-linux.txt"
grep "  $artifact\$" "$tmp/checksums-linux.txt" >"$tmp/checksums-selected.txt"
(cd "$tmp" && sha256sum -c checksums-selected.txt)
tar -xzf "$tmp/$artifact" -C "$tmp"
install -m 0755 "$tmp/bao" /usr/local/bin/bao
ln -sf /usr/local/bin/bao /usr/bin/bao

if ! getent passwd openbao >/dev/null; then
	useradd --system --home /var/lib/openbao --shell /usr/sbin/nologin openbao
fi

install -d -m 0750 -o openbao -g openbao /var/lib/openbao
install -d -m 0750 -o openbao -g openbao /var/lib/openbao/data
install -d -m 0750 -o openbao -g openbao /etc/openbao.d
install -d -m 0750 -o openbao -g openbao /etc/openbao.d/tls
install -d -m 0700 "$OPENBAO_LAB_DIR"

if [ ! -f /etc/openbao.d/tls/server.crt ]; then
	openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 30 \
		-subj "/CN=${OPENBAO_TLS_SERVER_NAME}" \
		-addext "subjectAltName=DNS:${OPENBAO_TLS_SERVER_NAME},DNS:localhost,IP:${OPENBAO_IP},IP:127.0.0.1" \
		-keyout /etc/openbao.d/tls/server.key \
		-out /etc/openbao.d/tls/server.crt
	chown openbao:openbao /etc/openbao.d/tls/server.key /etc/openbao.d/tls/server.crt
	chmod 0600 /etc/openbao.d/tls/server.key
	chmod 0644 /etc/openbao.d/tls/server.crt
fi
cp /etc/openbao.d/tls/server.crt /etc/openbao.d/tls/ca.crt
chmod 0644 /etc/openbao.d/tls/ca.crt

cat >/etc/openbao.d/openbao.hcl <<EOF
ui = false
api_addr = "https://${OPENBAO_IP}:8200"
cluster_addr = "https://${OPENBAO_IP}:8201"
disable_mlock = true

storage "file" {
  path = "/var/lib/openbao/data"
}

listener "tcp" {
  address = "0.0.0.0:8200"
  tls_cert_file = "/etc/openbao.d/tls/server.crt"
  tls_key_file = "/etc/openbao.d/tls/server.key"
}
EOF
chown root:openbao /etc/openbao.d/openbao.hcl
chmod 0640 /etc/openbao.d/openbao.hcl

cat >/etc/systemd/system/openbao.service <<'EOF'
[Unit]
Description=OpenBao local lab server
Documentation=https://openbao.org/docs/
After=network-online.target
Wants=network-online.target

[Service]
User=openbao
Group=openbao
ExecStart=/usr/local/bin/bao server -config=/etc/openbao.d/openbao.hcl
Restart=on-failure
RestartSec=2s
LimitNOFILE=65536
MemorySwapMax=0
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/var/lib/openbao

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now openbao.service

export BAO_ADDR="https://127.0.0.1:8200"
export BAO_CACERT="/etc/openbao.d/tls/ca.crt"

for _ in $(seq 1 60); do
	if curl -fsS --cacert "$BAO_CACERT" "$BAO_ADDR/v1/sys/health?standbyok=true&sealedcode=200&uninitcode=200" >/dev/null; then
		break
	fi
	sleep 2
done

if ! bao status -format=json | jq -e '.initialized == true' >/dev/null; then
	bao operator init -key-shares=1 -key-threshold=1 -format=json >"$OPENBAO_LAB_DIR/init.json"
	chmod 0600 "$OPENBAO_LAB_DIR/init.json"
fi

if bao status -format=json | jq -e '.sealed == true' >/dev/null; then
	unseal_key="$(jq -r '.unseal_keys_b64[0]' "$OPENBAO_LAB_DIR/init.json")"
	bao operator unseal "$unseal_key" >/dev/null
fi

printf '%s\n' "OpenBao installed and unsealed"
