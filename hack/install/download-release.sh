#!/usr/bin/env bash
# Download one pinned release artifact. Never install or activate it.
set -euo pipefail

if [[ $# != 4 ]]; then
  echo 'usage: download-release.sh VERSION systemd|static-pod|deb|rpm amd64|arm64 NEW_DIRECTORY' >&2
  exit 2
fi
version=$1
model=$2
arch=$3
destination=$4
if [[ ! "$version" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]; then
  echo 'VERSION must be a complete release tag, for example 0.1.0-preview.2' >&2
  exit 2
fi
case "$arch" in amd64|arm64) ;; *) echo 'architecture must be amd64 or arm64' >&2; exit 2 ;; esac
case "$model" in
  systemd|static-pod) artifact="bao-kms-provider_${version}_${model}_linux_${arch}.tar.gz" ;;
  deb|rpm) artifact="bao-kms-provider_${version}_linux_${arch}.${model}" ;;
  *) echo 'deployment model must be systemd, static-pod, deb, or rpm' >&2; exit 2 ;;
esac
for tool in gh cosign sha256sum awk; do
  command -v "$tool" >/dev/null || { echo "required command missing: $tool" >&2; exit 2; }
done
# Requiring a new directory prevents stale verification records or mixed versions.
mkdir -- "$destination"
cd -- "$destination"
repo=dc-tec/openbao-kubernetes-kms
identity="https://github.com/${repo}/.github/workflows/release.yml@refs/tags/${version}"

gh release download "$version" --repo "$repo" \
  --pattern "$artifact" --pattern checksums.txt --pattern checksums.txt.bundle
cosign verify-blob --new-bundle-format=true --bundle checksums.txt.bundle \
  --certificate-identity "$identity" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
# Require exactly one checksum for the requested file. Missing files cannot pass.
awk -v file="$artifact" '$2 == file { print; count++ } END { if (count != 1) exit 1 }' \
  checksums.txt > selected-checksum.txt
sha256sum --check selected-checksum.txt
gh attestation verify "./$artifact" --repo "$repo" \
  --signer-workflow "$repo/.github/workflows/release.yml" \
  --source-ref "refs/tags/$version" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com --deny-self-hosted-runners
printf 'Verified %s from %s at %s\n' "$artifact" "$repo" "$version"
printf 'No files were installed. Keep this directory for transfer and installation.\n'
