#!/bin/sh
set -eu

KUBERNETES_VERSION="${KUBERNETES_VERSION:-1.34.3}"
KUBERNETES_MINOR="${KUBERNETES_MINOR:-$(printf '%s\n' "$KUBERNETES_VERSION" | awk -F. '{print $1 "." $2}')}"
KUBEADM_NODE_IP="${KUBEADM_NODE_IP:?KUBEADM_NODE_IP is required}"
KUBEADM_CLUSTER_MODE="${KUBEADM_CLUSTER_MODE:-single}"
KUBEADM_CONTROL_PLANE_ENDPOINT="${KUBEADM_CONTROL_PLANE_ENDPOINT:-}"
KUBEADM_JOIN_COMMAND="${KUBEADM_JOIN_COMMAND:-}"
KUBEADM_CERTIFICATE_KEY="${KUBEADM_CERTIFICATE_KEY:-}"
KUBEADM_POD_CIDR="${KUBEADM_POD_CIDR:-10.244.0.0/16}"
KUBEADM_SERVICE_CIDR="${KUBEADM_SERVICE_CIDR:-10.96.0.0/12}"
KUBEADM_INSTALL_CNI="${KUBEADM_INSTALL_CNI:-true}"
FLANNEL_VERSION="${FLANNEL_VERSION:-v0.27.4}"

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y apt-transport-https ca-certificates curl gpg jq

swapoff -a || true
sed -i.bak '/[[:space:]]swap[[:space:]]/ s/^/#/' /etc/fstab || true

cat >/etc/modules-load.d/openbao-kms-kubeadm.conf <<'EOF'
overlay
br_netfilter
EOF
modprobe overlay || true
modprobe br_netfilter || true

cat >/etc/sysctl.d/99-openbao-kms-kubeadm.conf <<'EOF'
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward = 1
EOF
sysctl --system >/dev/null

apt-get install -y containerd
install -d -m 0755 /etc/containerd
containerd config default >/etc/containerd/config.toml
sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
cat >/etc/crictl.yaml <<'EOF'
runtime-endpoint: unix:///run/containerd/containerd.sock
image-endpoint: unix:///run/containerd/containerd.sock
timeout: 10
debug: false
EOF
systemctl enable --now containerd

install -d -m 0755 /etc/apt/keyrings
rm -f /etc/apt/keyrings/kubernetes-apt-keyring.gpg
curl -fsSL "https://pkgs.k8s.io/core:/stable:/v${KUBERNETES_MINOR}/deb/Release.key" \
	| gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
chmod 0644 /etc/apt/keyrings/kubernetes-apt-keyring.gpg
cat >/etc/apt/sources.list.d/kubernetes.list <<EOF
deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v${KUBERNETES_MINOR}/deb/ /
EOF

apt-get update
kubernetes_package_version="$(apt-cache madison kubeadm | awk -v version="$KUBERNETES_VERSION" '$3 ~ "^" version "-" { print $3; exit }')"
if [ -z "$kubernetes_package_version" ]; then
	printf 'kubeadm package version not found for Kubernetes %s\n' "$KUBERNETES_VERSION" >&2
	apt-cache madison kubeadm >&2
	exit 1
fi

apt-get install -y \
	"kubelet=${kubernetes_package_version}" \
	"kubeadm=${kubernetes_package_version}" \
	"kubectl=${kubernetes_package_version}"
apt-mark hold kubelet kubeadm kubectl >/dev/null
systemctl enable --now kubelet

if [ ! -f /etc/kubernetes/admin.conf ]; then
	kubeadm config images pull --kubernetes-version "v${KUBERNETES_VERSION}"
	case "$KUBEADM_CLUSTER_MODE" in
	single | init)
		kubeadm_init_log="/var/log/openbao-kms-kubeadm-init.log"
		set -- kubeadm init \
			--kubernetes-version "v${KUBERNETES_VERSION}" \
			--apiserver-advertise-address "$KUBEADM_NODE_IP" \
			--node-name "$(hostname)" \
			--pod-network-cidr "$KUBEADM_POD_CIDR" \
			--service-cidr "$KUBEADM_SERVICE_CIDR"
		if [ "$KUBEADM_CLUSTER_MODE" = "init" ]; then
			if [ -z "$KUBEADM_CONTROL_PLANE_ENDPOINT" ]; then
				printf '%s\n' 'KUBEADM_CONTROL_PLANE_ENDPOINT is required for init mode' >&2
				exit 1
			fi
			set -- "$@" --control-plane-endpoint "$KUBEADM_CONTROL_PLANE_ENDPOINT" --upload-certs
		fi
		if ! "$@" >"$kubeadm_init_log" 2>&1; then
			tail -80 "$kubeadm_init_log" >&2
			exit 1
		fi
		chmod 0600 "$kubeadm_init_log"
		printf 'kubeadm initialized; detailed log: %s\n' "$kubeadm_init_log"
		;;
	join)
		if [ -z "$KUBEADM_JOIN_COMMAND" ] || [ -z "$KUBEADM_CERTIFICATE_KEY" ]; then
			printf '%s\n' 'KUBEADM_JOIN_COMMAND and KUBEADM_CERTIFICATE_KEY are required for join mode' >&2
			exit 1
		fi
		kubeadm_join_log="/var/log/openbao-kms-kubeadm-join.log"
		if ! sh -c "$KUBEADM_JOIN_COMMAND --control-plane --certificate-key '$KUBEADM_CERTIFICATE_KEY' --apiserver-advertise-address '$KUBEADM_NODE_IP' --node-name '$(hostname)'" >"$kubeadm_join_log" 2>&1; then
			tail -80 "$kubeadm_join_log" >&2
			exit 1
		fi
		chmod 0600 "$kubeadm_join_log"
		printf 'kubeadm joined control plane; detailed log: %s\n' "$kubeadm_join_log"
		;;
	*)
		printf 'unsupported KUBEADM_CLUSTER_MODE: %s\n' "$KUBEADM_CLUSTER_MODE" >&2
		exit 1
		;;
	esac
fi

install -d -m 0700 -o ubuntu -g ubuntu /home/ubuntu/.kube
cp /etc/kubernetes/admin.conf /home/ubuntu/.kube/config
chown ubuntu:ubuntu /home/ubuntu/.kube/config
chmod 0600 /home/ubuntu/.kube/config

export KUBECONFIG=/etc/kubernetes/admin.conf
kubectl taint nodes --all node-role.kubernetes.io/control-plane- >/dev/null 2>&1 || true

if [ "$KUBEADM_INSTALL_CNI" = "true" ]; then
	kubectl apply -f "https://github.com/flannel-io/flannel/releases/download/${FLANNEL_VERSION}/kube-flannel.yml"
fi

if [ "$KUBEADM_INSTALL_CNI" = "true" ] || [ "$KUBEADM_CLUSTER_MODE" = "join" ]; then
	kubectl wait node "$(hostname)" --for=condition=Ready --timeout="${KUBEADM_NODE_READY_TIMEOUT:-10m}"
fi

kubectl get nodes -o wide
