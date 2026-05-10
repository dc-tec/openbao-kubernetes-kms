#!/usr/bin/env bash

set -euo pipefail

PRIMARY_DIR="${PRIMARY_DIR:-dist/primary}"
REBUILD_DIR="${REBUILD_DIR:-dist/rebuild}"
REPRO_REQUIRED_FILES="${REPRO_REQUIRED_FILES:-}"
REPRO_OPTIONAL_FILES="${REPRO_OPTIONAL_FILES:-}"

if [[ ! -d "${PRIMARY_DIR}" ]]; then
  echo "primary artifact directory not found: ${PRIMARY_DIR}" >&2
  exit 1
fi
if [[ ! -d "${REBUILD_DIR}" ]]; then
  echo "rebuild artifact directory not found: ${REBUILD_DIR}" >&2
  exit 1
fi

status=0

sha256_file() {
  local path="$1"

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${path}" | awk '{print $1}'
    return
  fi
  shasum -a 256 "${path}" | awk '{print $1}'
}

compare_digest() {
  local label="$1"
  local primary="$2"
  local rebuild="$3"

  if [[ -z "${primary}" && -z "${rebuild}" ]]; then
    return
  fi
  if [[ -z "${primary}" || -z "${rebuild}" ]]; then
    echo "digest missing (${label}): primary=${primary:-<missing>} rebuild=${rebuild:-<missing>}" >&2
    status=1
    return
  fi
  if [[ "${primary}" != "${rebuild}" ]]; then
    echo "digest mismatch (${label}): primary=${primary} rebuild=${rebuild}" >&2
    status=1
    return
  fi
  echo "digest match (${label}): ${primary}"
}

compare_file() {
  local rel="$1"
  local allow_missing="$2"
  local primary_path="${PRIMARY_DIR}/${rel}"
  local rebuild_path="${REBUILD_DIR}/${rel}"

  if [[ ! -f "${primary_path}" || ! -f "${rebuild_path}" ]]; then
    if [[ "${allow_missing}" == "true" ]]; then
      echo "skipping optional file (missing in one or both dirs): ${rel}"
      return
    fi
    echo "required file missing for reproducibility check: ${rel}" >&2
    status=1
    return
  fi

  local primary_sha rebuild_sha
  primary_sha="$(sha256_file "${primary_path}")"
  rebuild_sha="$(sha256_file "${rebuild_path}")"

  if [[ "${primary_sha}" != "${rebuild_sha}" ]]; then
    echo "byte mismatch (${rel}): primary=${primary_sha} rebuild=${rebuild_sha}" >&2
    status=1
    return
  fi
  echo "byte match (${rel}): ${primary_sha}"
}

compare_digest "bao-kms-provider image" "${IMAGE_DIGEST:-}" "${IMAGE_REBUILD_DIGEST:-}"

if [[ -n "${REPRO_REQUIRED_FILES}" ]]; then
  for rel in ${REPRO_REQUIRED_FILES}; do
    compare_file "${rel}" "false"
  done
else
  compare_file "checksums.txt" "false"
  while read -r _ rel; do
    [[ -n "${rel}" ]] || continue
    compare_file "${rel}" "false"
  done < "${PRIMARY_DIR}/checksums.txt"
fi

for rel in ${REPRO_OPTIONAL_FILES}; do
  compare_file "${rel}" "true"
done

if (( status != 0 )); then
  echo "byte reproducibility verification failed" >&2
  exit 1
fi

echo "byte reproducibility verification passed"
