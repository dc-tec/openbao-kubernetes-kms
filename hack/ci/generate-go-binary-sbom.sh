#!/usr/bin/env bash

set -euo pipefail

: "${BINARY_PATH:?BINARY_PATH is required}"
: "${OUTPUT_PATH:?OUTPUT_PATH is required}"
: "${VERSION:?VERSION is required}"
: "${GOOS:?GOOS is required}"
: "${GOARCH:?GOARCH is required}"
: "${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required}"

python3 - "${BINARY_PATH}" "${OUTPUT_PATH}" "${VERSION}" "${GOOS}" "${GOARCH}" "${SOURCE_DATE_EPOCH}" <<'PY'
import datetime
import hashlib
import json
import re
import sys
from pathlib import Path


binary_path = Path(sys.argv[1])
output_path = Path(sys.argv[2])
version = sys.argv[3]
goos = sys.argv[4]
goarch = sys.argv[5]
source_date_epoch = int(sys.argv[6])

created = datetime.datetime.fromtimestamp(
    source_date_epoch, datetime.timezone.utc
).strftime("%Y-%m-%dT%H:%M:%SZ")

binary_bytes = binary_path.read_bytes()
binary_sha = hashlib.sha256(binary_bytes).hexdigest()


def spdx_id(value):
    cleaned = re.sub(r"[^A-Za-z0-9.-]", "-", value)
    cleaned = re.sub(r"-+", "-", cleaned).strip("-")
    return cleaned or "unknown"


def module_entries():
    entries = []
    modules_path = Path("vendor/modules.txt")
    for line in modules_path.read_text(encoding="utf-8").splitlines():
        if not line.startswith("# "):
            continue
        fields = line[2:].split()
        if len(fields) < 2:
            continue
        if "=>" in fields:
            arrow = fields.index("=>")
            fields = fields[:arrow]
        if len(fields) < 2:
            continue
        entries.append({"Path": fields[0], "Version": fields[1]})
    return entries


modules = module_entries()
packages = [
    {
        "SPDXID": f"SPDXRef-Binary-bao-kms-provider-{goos}-{goarch}",
        "name": f"bao-kms-provider_{version}_{goos}_{goarch}",
        "versionInfo": version,
        "downloadLocation": "NOASSERTION",
        "filesAnalyzed": False,
        "licenseConcluded": "NOASSERTION",
        "licenseDeclared": "NOASSERTION",
        "copyrightText": "NOASSERTION",
        "checksums": [
            {
                "algorithm": "SHA256",
                "checksumValue": binary_sha,
            }
        ],
    }
]

relationships = [
    {
        "spdxElementId": "SPDXRef-DOCUMENT",
        "relationshipType": "DESCRIBES",
        "relatedSpdxElement": f"SPDXRef-Binary-bao-kms-provider-{goos}-{goarch}",
    }
]

seen = set()
for module in modules:
    path = module.get("Path", "")
    if not path:
        continue
    module_version = module.get("Version", "")
    package_id = "SPDXRef-GoModule-" + spdx_id(path + "-" + module_version)
    if package_id in seen:
        continue
    seen.add(package_id)
    packages.append(
        {
            "SPDXID": package_id,
            "name": path,
            "versionInfo": module_version or "local",
            "downloadLocation": "NOASSERTION",
            "filesAnalyzed": False,
            "licenseConcluded": "NOASSERTION",
            "licenseDeclared": "NOASSERTION",
            "copyrightText": "NOASSERTION",
            "externalRefs": [
                {
                    "referenceCategory": "PACKAGE-MANAGER",
                    "referenceType": "purl",
                    "referenceLocator": "pkg:golang/" + path + (("@" + module_version) if module_version else ""),
                }
            ],
        }
    )
    relationships.append(
        {
            "spdxElementId": f"SPDXRef-Binary-bao-kms-provider-{goos}-{goarch}",
            "relationshipType": "DEPENDS_ON",
            "relatedSpdxElement": package_id,
        }
    )

packages = sorted(packages, key=lambda item: item["SPDXID"])
relationships = sorted(
    relationships,
    key=lambda item: (
        item["spdxElementId"],
        item["relationshipType"],
        item["relatedSpdxElement"],
    ),
)

namespace_seed = f"{version}:{goos}:{goarch}:{binary_sha}"
namespace_hash = hashlib.sha256(namespace_seed.encode("utf-8")).hexdigest()

doc = {
    "spdxVersion": "SPDX-2.3",
    "dataLicense": "CC0-1.0",
    "SPDXID": "SPDXRef-DOCUMENT",
    "name": f"bao-kms-provider {version} {goos}/{goarch}",
    "documentNamespace": f"https://openbao-kubernetes-kms.dev/sbom/binary/{namespace_hash}",
    "creationInfo": {
        "created": created,
        "creators": [
            "Tool: openbao-kubernetes-kms-generate-go-binary-sbom",
        ],
    },
    "packages": packages,
    "relationships": relationships,
}

output_path.parent.mkdir(parents=True, exist_ok=True)
output_path.write_text(json.dumps(doc, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
PY
