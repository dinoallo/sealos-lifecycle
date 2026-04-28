#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
. "${SCRIPT_DIR}/common.sh"

consume_sealos_bin_flag "$@"
resolve_sealos_bin

exec "${SEALOS_BIN}" sync package build --output env "${FORWARD_ARGS[@]}"
