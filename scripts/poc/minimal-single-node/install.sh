#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

CLUSTER_NAME="poc-minimal"
BUNDLE_DIR=""
KUBECONFIG_PATH="/etc/kubernetes/admin.conf"
SEALOS_HOME="${SEALOS_HOME:-${HOME}/.sealos}"
SEALOS_BIN="${REPO_ROOT}/bin/linux_amd64/sealos"
SKIP_VALIDATE=0

usage() {
  cat <<'EOF'
Usage:
  install.sh [--cluster NAME] [--bundle-dir DIR] [--kubeconfig PATH] [--sealos-bin PATH] [--skip-validate]

Legacy compatibility wrapper around `sealos sync apply` for the minimal
single-node PoC.

The default prepared-host path is:
  scripts/poc/minimal-single-node/bootstrap.sh

The rendered bundle validation now lives in `sealos sync apply`, including
checks for placeholder or incomplete staged assets before host mutation.
EOF
}

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --cluster)
        CLUSTER_NAME="${2:?missing value for --cluster}"
        shift 2
        ;;
      --bundle-dir)
        BUNDLE_DIR="${2:?missing value for --bundle-dir}"
        shift 2
        ;;
      --kubeconfig)
        KUBECONFIG_PATH="${2:?missing value for --kubeconfig}"
        shift 2
        ;;
      --sealos-bin)
        SEALOS_BIN="${2:?missing value for --sealos-bin}"
        shift 2
        ;;
      --skip-validate)
        SKIP_VALIDATE=1
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "unknown argument: $1"
        ;;
    esac
  done
}

resolve_bundle_dir() {
  if [[ -n "${BUNDLE_DIR}" ]]; then
    printf '%s\n' "${BUNDLE_DIR}"
    return
  fi
  printf '%s\n' "${SEALOS_HOME}/${CLUSTER_NAME}/distribution/bundles/current"
}

resolve_sealos_bin() {
  if [[ -x "${SEALOS_BIN}" ]]; then
    return
  fi

  if command -v sealos >/dev/null 2>&1; then
    SEALOS_BIN="$(command -v sealos)"
    return
  fi

  fail "sealos binary not found; build it first or pass --sealos-bin"
}

main() {
  parse_args "$@"
  resolve_sealos_bin

  RESOLVED_BUNDLE_DIR="$(resolve_bundle_dir)"
  readonly RESOLVED_BUNDLE_DIR

  log "using rendered bundle at ${RESOLVED_BUNDLE_DIR}"
  log "delegating host apply to sealos sync apply"
  "${SEALOS_BIN}" sync apply \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${RESOLVED_BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}"

  if (( SKIP_VALIDATE == 0 )); then
    log "running post-apply validation"
    "${SCRIPT_DIR}/validate.sh" \
      --cluster "${CLUSTER_NAME}" \
      --bundle-dir "${RESOLVED_BUNDLE_DIR}" \
      --kubeconfig "${KUBECONFIG_PATH}"
  fi
}

main "$@"
