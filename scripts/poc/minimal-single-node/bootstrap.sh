#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
SEALOS_HOME="${SEALOS_HOME:-${HOME}/.sealos}"

CLUSTER_NAME="poc-minimal"
ASSETS_WORKDIR="${SCRIPT_DIR}/artifacts"
KUBECONFIG_PATH="/etc/kubernetes/admin.conf"
SEALOS_BIN="${REPO_ROOT}/bin/linux_amd64/sealos"
SKIP_BUILD=0
SKIP_INSTALL=0
SKIP_VALIDATE=0

FETCH_ENV_FILE=""

usage() {
  cat <<'EOF'
Usage:
  bootstrap.sh [options]

Runs the minimal single-node PoC end-to-end:
  1. build sealos
  2. fetch assets
  3. stage assets into package directories
  4. render the bundle
  5. install the rendered bundle
  6. validate the cluster

Options:
  --cluster NAME
  --assets-workdir DIR
  --kubeconfig PATH
  --sealos-bin PATH
  --skip-build
  --skip-install
  --skip-validate
EOF
}

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [[ -n "${FETCH_ENV_FILE}" && -f "${FETCH_ENV_FILE}" ]]; then
    rm -f "${FETCH_ENV_FILE}"
  fi
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --cluster)
        CLUSTER_NAME="${2:?missing value for --cluster}"
        shift 2
        ;;
      --assets-workdir)
        ASSETS_WORKDIR="${2:?missing value for --assets-workdir}"
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
      --skip-build)
        SKIP_BUILD=1
        shift
        ;;
      --skip-install)
        SKIP_INSTALL=1
        shift
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

require_root_if_needed() {
  if (( SKIP_INSTALL == 0 )) && [[ "${EUID}" -ne 0 ]]; then
    fail "bootstrap install path must run as root"
  fi
}

ensure_command() {
  local name="$1"
  command -v "${name}" >/dev/null 2>&1 || fail "required command not found: ${name}"
}

ensure_swap_disabled() {
  if swapon --noheadings --show | grep -q .; then
    fail "swap must be disabled before running bootstrap.sh"
  fi
}

ensure_systemd_ready() {
  local state

  state="$(systemctl is-system-running 2>/dev/null || true)"
  case "${state}" in
    running|degraded)
      return
      ;;
    *)
      fail "systemd is not ready for host mutation (state: ${state:-unknown})"
      ;;
  esac
}

preflight_host() {
  if (( SKIP_INSTALL != 0 )); then
    return
  fi

  log "checking host prerequisites"
  ensure_command systemctl
  ensure_command modprobe
  ensure_command sysctl
  ensure_command swapon
  ensure_command conntrack
  ensure_command crictl
  ensure_command socat

  ensure_swap_disabled
  ensure_systemd_ready
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

build_sealos() {
  if (( SKIP_BUILD != 0 )); then
    log "skipping sealos build"
    return
  fi

  ensure_command make
  ensure_command go

  log "building sealos"
  (
    cd "${REPO_ROOT}"
    make build BINS=sealos
  )
}

fetch_assets() {
  FETCH_ENV_FILE="$(mktemp)"
  readonly FETCH_ENV_FILE

  log "fetching PoC assets"
  "${SCRIPT_DIR}/fetch-assets.sh" --workdir "${ASSETS_WORKDIR}" > "${FETCH_ENV_FILE}"
}

stage_assets() {
  log "staging assets into package directories"

  set -a
  # shellcheck disable=SC1090
  . "${FETCH_ENV_FILE}"
  set +a

  "${SCRIPT_DIR}/stage-assets.sh" \
    --containerd-bin "${containerd_bin}" \
    --containerd-shim-bin "${containerd_shim_bin}" \
    --ctr-bin "${ctr_bin}" \
    --runc-bin "${runc_bin}" \
    --kubeadm-bin "${kubeadm_bin}" \
    --kubelet-bin "${kubelet_bin}" \
    --kubectl-bin "${kubectl_bin}" \
    --cilium-manifest "${cilium_manifest}"
}

render_bundle() {
  log "rendering desired-state bundle"
  SEALOS_BIN="${SEALOS_BIN}" "${SCRIPT_DIR}/render.sh" --cluster "${CLUSTER_NAME}" --sealos-bin "${SEALOS_BIN}"
}

install_bundle() {
  log "installing rendered bundle"
  "${SCRIPT_DIR}/install.sh" \
    --cluster "${CLUSTER_NAME}" \
    --kubeconfig "${KUBECONFIG_PATH}" \
    --skip-validate
}

validate_cluster() {
  log "validating rendered bundle"
  "${SCRIPT_DIR}/validate.sh" \
    --cluster "${CLUSTER_NAME}" \
    --kubeconfig "${KUBECONFIG_PATH}"
}

main() {
  trap cleanup EXIT
  parse_args "$@"
  if (( SKIP_INSTALL != 0 )); then
    SKIP_VALIDATE=1
  fi
  require_root_if_needed
  preflight_host

  build_sealos
  resolve_sealos_bin
  fetch_assets
  stage_assets
  render_bundle

  if (( SKIP_INSTALL == 0 )); then
    install_bundle
  else
    log "skipping install"
  fi

  if (( SKIP_VALIDATE == 0 )); then
    validate_cluster
  else
    log "skipping validate"
  fi

  log "bootstrap completed for cluster ${CLUSTER_NAME}"
  printf 'bundle dir: %s\n' "${SEALOS_HOME}/${CLUSTER_NAME}/distribution/bundles/current"
}

main "$@"
