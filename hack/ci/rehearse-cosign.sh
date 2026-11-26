#!/usr/bin/env bash
# Exercise an installed candidate with synthetic local inputs only.
set -euo pipefail
umask 077

evidence="${RUNNER_TEMP:?}/kms-cosign-evidence"
mkdir -p "$evidence"
scratch="$(mktemp -d "${RUNNER_TEMP}/kms-cosign-key-XXXXXX")"
trap 'rm -rf "$scratch"' EXIT

cosign version --json > "$evidence/version.json"
python3 - "$evidence/version.json" <<'PY'
import json, sys
assert json.load(open(sys.argv[1]))['gitVersion'] == 'v3.0.4'
PY
sha256sum "$(command -v cosign)" > "$evidence/installed-binary.sha256"
cd "$scratch"
export COSIGN_PASSWORD=''
cosign generate-key-pair --output-key-prefix synthetic
printf '%s\n' 'KMS action rehearsal: synthetic local blob' > blob.txt
# The detached local-key path disables the signing service configuration and
# transparency-log upload. It does not exercise production keyless signing.
cosign sign-blob --yes --key synthetic.key --use-signing-config=false \
  --new-bundle-format=false --tlog-upload=false --output-signature blob.sig blob.txt
cosign verify-blob --key synthetic.pub --new-bundle-format=false \
  --insecure-ignore-tlog --offline --signature blob.sig blob.txt
printf '%s\n' 'tampered synthetic blob' > tampered.txt
if cosign verify-blob --key synthetic.pub --new-bundle-format=false \
  --insecure-ignore-tlog --offline --signature blob.sig tampered.txt; then
  echo 'Cosign accepted a modified blob' >&2
  exit 1
fi
cp blob.txt blob.sig synthetic.pub "$evidence/"
printf '%s\n' '{"installed":"v3.0.4","bootstrap":"v3.1.4","validBlob":"verified","tamperedBlob":"rejected","transparencyLogUpload":false,"keyless":false}' \
  > "$evidence/result.json"
cat "$evidence/result.json"
