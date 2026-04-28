#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
SEALOS_BIN="${SEALOS_BIN:-}"
FORWARD_ARGS=()

log() {
  printf '==> %s\n' "$*" >&2
}

print_kv() {
  printf '%s=%q\n' "$1" "$2"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

consume_sealos_bin_flag() {
  FORWARD_ARGS=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --sealos-bin)
        [[ $# -ge 2 ]] || fail "missing value for --sealos-bin"
        SEALOS_BIN="$2"
        shift 2
        ;;
      --sealos-bin=*)
        SEALOS_BIN="${1#*=}"
        shift
        ;;
      *)
        FORWARD_ARGS+=("$1")
        shift
        ;;
    esac
  done
}

resolve_sealos_bin() {
  if [[ -n "${SEALOS_BIN}" ]]; then
    [[ -x "${SEALOS_BIN}" ]] || fail "sealos binary is not executable: ${SEALOS_BIN}"
    return
  fi

  if [[ -x "${REPO_ROOT}/bin/linux_amd64/sealos" ]]; then
    SEALOS_BIN="${REPO_ROOT}/bin/linux_amd64/sealos"
    return
  fi

  if command -v sealos >/dev/null 2>&1; then
    SEALOS_BIN="$(command -v sealos)"
    return
  fi

  fail "sealos binary not found; build it first or pass --sealos-bin"
}
