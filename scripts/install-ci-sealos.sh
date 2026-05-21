#!/bin/bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SEALOS_VERSION=${SEALOS_VERSION:-v5.1.0-beta3}

if [[ "${SEALOS_VERSION}" == "v5.1.0-beta3" && -z "${SEALOS_INSTALL_SKIP_CHECKSUM:-}" ]]; then
  export SEALOS_INSTALL_SKIP_CHECKSUM=true
fi

bash "${SCRIPT_DIR}/install.sh" "${SEALOS_VERSION}" "${SEALOS_REPO:-labring/sealos}"
