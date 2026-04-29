#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
SEALOS_HOME="${SEALOS_HOME:-${HOME}/.sealos}"

CLUSTER_NAME="poc-minimal"
ASSETS_WORKDIR="${SCRIPT_DIR}/artifacts"
KUBECONFIG_PATH="/etc/kubernetes/admin.conf"
SEALOS_BIN="${REPO_ROOT}/bin/linux_amd64/sealos"
REGISTRY_PORT="5065"
SKIP_BUILD=0
SKIP_APPLY=0
SKIP_VALIDATE=0

PUBLISH_ENV_FILE=""
REGISTRY_PID=""
REGISTRY_PREFIX=""
REGISTRY_LOG=""

usage() {
  cat <<'EOF'
Usage:
  bootstrap.sh [options]

Runs the minimal single-node PoC on a prepared Linux host:
  1. build sealos
  2. start a temporary local registry
  3. publish the three PoC package images to OCI
  4. render the bundle from the generated OCI-backed BOM
  5. apply the rendered bundle with sealos sync apply
  6. validate the cluster

This wrapper expects host prerequisites to already be in place:
  - systemd is running
  - swap is disabled
  - conntrack, crictl, and socat are installed

Options:
  --cluster NAME
  --assets-workdir DIR
  --kubeconfig PATH
  --registry-port PORT
  --sealos-bin PATH
  --skip-build
  --skip-apply
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
  if [[ -n "${PUBLISH_ENV_FILE}" && -f "${PUBLISH_ENV_FILE}" ]]; then
    rm -f "${PUBLISH_ENV_FILE}"
  fi
  if [[ -n "${REGISTRY_PID}" ]] && kill -0 "${REGISTRY_PID}" >/dev/null 2>&1; then
    kill "${REGISTRY_PID}" >/dev/null 2>&1 || true
    wait "${REGISTRY_PID}" >/dev/null 2>&1 || true
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
      --registry-port)
        REGISTRY_PORT="${2:?missing value for --registry-port}"
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
      --skip-apply)
        SKIP_APPLY=1
        shift
        ;;
      --skip-install)
        SKIP_APPLY=1
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
  if (( SKIP_APPLY == 0 )) && [[ "${EUID}" -ne 0 ]]; then
    fail "bootstrap apply path must run as root"
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
  if (( SKIP_APPLY != 0 )); then
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
  ensure_command curl

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

start_registry() {
  local registry_dir="${ASSETS_WORKDIR}/registry"
  REGISTRY_LOG="${ASSETS_WORKDIR}/registry.log"
  REGISTRY_PREFIX="localhost:${REGISTRY_PORT}/${CLUSTER_NAME}"

  ensure_command curl
  install -d "${registry_dir}"

  log "starting temporary local registry on port ${REGISTRY_PORT}"
  "${SEALOS_BIN}" registry serve filesystem "${registry_dir}" --port "${REGISTRY_PORT}" >"${REGISTRY_LOG}" 2>&1 &
  REGISTRY_PID=$!

  local attempt
  for attempt in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${REGISTRY_PORT}/v2/" >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done

  fail "temporary registry did not become ready on port ${REGISTRY_PORT}; see ${REGISTRY_LOG}"
}

publish_packages() {
  PUBLISH_ENV_FILE="$(mktemp)"
  readonly PUBLISH_ENV_FILE

  log "publishing OCI package images"
  SEALOS_BIN="${SEALOS_BIN}" "${SCRIPT_DIR}/publish-oci.sh" \
    --registry-prefix "${REGISTRY_PREFIX}" \
    --workdir "${ASSETS_WORKDIR}/oci" > "${PUBLISH_ENV_FILE}"
}

render_bundle() {
  log "rendering desired-state bundle from OCI-backed BOM"

  set -a
  # shellcheck disable=SC1090
  . "${PUBLISH_ENV_FILE}"
  set +a

  SEALOS_BIN="${SEALOS_BIN}" "${SCRIPT_DIR}/render.sh" \
    --cluster "${CLUSTER_NAME}" \
    --package-mode oci \
    --bom-file "${bom_path}" \
    --sealos-bin "${SEALOS_BIN}"
}

apply_bundle() {
  local bundle_dir="${SEALOS_HOME}/${CLUSTER_NAME}/distribution/bundles/current"

  log "applying rendered bundle with sealos sync apply"
  "${SEALOS_BIN}" sync apply \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${bundle_dir}" \
    --kubeconfig "${KUBECONFIG_PATH}"
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
  if (( SKIP_APPLY != 0 )); then
    SKIP_VALIDATE=1
  fi
  require_root_if_needed
  preflight_host

  build_sealos
  resolve_sealos_bin
  start_registry
  publish_packages
  render_bundle
  cleanup
  trap cleanup EXIT

  if (( SKIP_APPLY == 0 )); then
    apply_bundle
  else
    log "skipping apply"
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
