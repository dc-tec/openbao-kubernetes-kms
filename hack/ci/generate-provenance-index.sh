#!/usr/bin/env bash

set -euo pipefail

: "${REPO:?REPO is required (owner/repo)}"
: "${OWNER:?OWNER is required}"
: "${VERSION:?VERSION is required}"
: "${IMAGE:?IMAGE is required}"
: "${IMAGE_DIGEST:?IMAGE_DIGEST is required}"

INDEX_PATH="${INDEX_PATH:-dist/provenance-index.json}"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-0}"
CHECKSUMS_PATH="${CHECKSUMS_PATH:-dist/checksums.txt}"
CHECKSUMS_BUNDLE_PATH="${CHECKSUMS_BUNDLE_PATH:-dist/checksums.txt.bundle}"
SBOM_GLOB="${SBOM_GLOB:-dist/sbom-*.spdx.json}"
BINARY_NAME="${BINARY_NAME:-bao-kms-provider}"
RELEASE_SOURCE_REF="${RELEASE_SOURCE_REF:-refs/tags/${VERSION}}"
RELEASE_WORKFLOW="${RELEASE_WORKFLOW:-${REPO}/.github/workflows/release.yml}"

GOFLAGS="${GOFLAGS:--mod=vendor}" go run ./hack/tools/provenance_index \
  -index-path "${INDEX_PATH}" \
  -repo "${REPO}" \
  -owner "${OWNER}" \
  -version "${VERSION}" \
  -source-date-epoch "${SOURCE_DATE_EPOCH}" \
  -binary-name "${BINARY_NAME}" \
  -image "${IMAGE}" \
  -image-digest "${IMAGE_DIGEST}" \
  -image-rebuild-digest "${IMAGE_REBUILD_DIGEST:-}" \
  -release-source-ref "${RELEASE_SOURCE_REF}" \
  -release-workflow "${RELEASE_WORKFLOW}" \
  -checksums-path "${CHECKSUMS_PATH}" \
  -checksums-bundle-path "${CHECKSUMS_BUNDLE_PATH}" \
  -sbom-glob "${SBOM_GLOB}"
