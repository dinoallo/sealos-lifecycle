#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CLUSTER_NAME="poc-minimal"
SEALOS_BIN="${SEALOS_BIN:-}"

usage() {
  cat <<'EOF'
Usage:
  render.sh [--cluster NAME] [--sealos-bin PATH]

Renders the minimal single-node PoC bundle from local package directories.
EOF
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
      --sealos-bin)
        SEALOS_BIN="${2:?missing value for --sealos-bin}"
        shift 2
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

main() {
  parse_args "$@"
  resolve_sealos_bin

  exec "${SEALOS_BIN}" sync render \
    --file "${REPO_ROOT}/scripts/poc/minimal-single-node/bom.yaml" \
    --cluster "${CLUSTER_NAME}" \
    --package-source "containerd=${REPO_ROOT}/scripts/poc/minimal-single-node/packages/containerd" \
    --package-source "kubernetes=${REPO_ROOT}/scripts/poc/minimal-single-node/packages/kubernetes" \
    --package-source "cilium=${REPO_ROOT}/scripts/poc/minimal-single-node/packages/cilium"
}

main "$@"
