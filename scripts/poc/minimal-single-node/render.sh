#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
POC_DIR="${REPO_ROOT}/scripts/poc/minimal-single-node"
CLUSTER_NAME="poc-minimal"
SEALOS_BIN="${SEALOS_BIN:-}"
BOM_FILE="${POC_DIR}/bom.yaml"
DEFAULT_LOCAL_BOM="${POC_DIR}/bom.yaml"
DEFAULT_OCI_BOM="${POC_DIR}/artifacts/oci/bom.oci.yaml"
PACKAGE_MODE="auto"

usage() {
  cat <<'EOF'
Usage:
  render.sh [--cluster NAME] [--bom-file PATH] [--package-mode MODE] [--sealos-bin PATH]

Renders the minimal single-node PoC bundle.

Package modes:
  auto   Prefer the generated OCI BOM when present, otherwise use local packages.
  local  Render from in-tree package directories via --package-source overrides.
  oci    Render from the BOM artifact image and digest references directly.
         Defaults to artifacts/oci/bom.oci.yaml when present.
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
      --bom-file)
        BOM_FILE="${2:?missing value for --bom-file}"
        shift 2
        ;;
      --package-mode)
        PACKAGE_MODE="${2:?missing value for --package-mode}"
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

  local resolved_mode="${PACKAGE_MODE}"
  case "${PACKAGE_MODE}" in
    auto)
      if [[ "${BOM_FILE}" == "${DEFAULT_LOCAL_BOM}" && -f "${DEFAULT_OCI_BOM}" ]]; then
        BOM_FILE="${DEFAULT_OCI_BOM}"
        resolved_mode="oci"
      else
        resolved_mode="local"
      fi
      ;;
    oci)
      if [[ "${BOM_FILE}" == "${DEFAULT_LOCAL_BOM}" ]]; then
        if [[ -f "${DEFAULT_OCI_BOM}" ]]; then
          BOM_FILE="${DEFAULT_OCI_BOM}"
        else
          fail "oci package mode requires --bom-file or a generated BOM at ${DEFAULT_OCI_BOM}; run publish-oci.sh first"
        fi
      fi
      ;;
    local)
      ;;
    *)
      fail "unsupported package mode: ${PACKAGE_MODE} (want auto, local, or oci)"
      ;;
  esac

  [[ -f "${BOM_FILE}" ]] || fail "bom file not found: ${BOM_FILE}"

  local -a args=(
    sync
    render
    --file "${BOM_FILE}"
    --cluster "${CLUSTER_NAME}"
  )

  case "${resolved_mode}" in
    local)
      args+=(
        --package-source "containerd=${REPO_ROOT}/scripts/poc/minimal-single-node/packages/containerd"
        --package-source "kubernetes=${REPO_ROOT}/scripts/poc/minimal-single-node/packages/kubernetes"
        --package-source "cilium=${REPO_ROOT}/scripts/poc/minimal-single-node/packages/cilium"
      )
      ;;
    oci)
      ;;
  esac

  exec "${SEALOS_BIN}" "${args[@]}"
}

main "$@"
