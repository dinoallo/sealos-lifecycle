#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
SEALOS_HOME="${SEALOS_HOME:-${HOME}/.sealos}"

CLUSTER_NAME="poc-minimal"
ASSETS_WORKDIR="${SCRIPT_DIR}/artifacts"
RUNTIME_ROOT=""
LOCAL_REPO=""
KUBECONFIG_PATH="/etc/kubernetes/admin.conf"
SEALOS_BIN="${REPO_ROOT}/bin/linux_amd64/sealos"
REGISTRY_PORT="5065"
RELEASE_LINE="minimal-single-node"
RELEASE_CHANNEL="alpha"
SKIP_BUILD=0
SKIP_APPLY=0
SKIP_VALIDATE=0

PUBLISH_ENV_FILE=""
REGISTRY_PID=""
REGISTRY_PREFIX=""
REGISTRY_LOG=""
BUNDLE_DIR=""

usage() {
  cat <<'EOF'
Usage:
  bootstrap.sh [options]

Runs the minimal single-node PoC on a prepared Linux host:
  1. build sealos
  2. start a temporary local registry
  3. publish the three PoC package images to OCI
  4. render the bundle through the generated release metadata source
  5. apply the rendered bundle with sealos sync apply
  6. validate the cluster

This wrapper expects host prerequisites to already be in place:
  - systemd is running
  - swap is disabled
  - conntrack, crictl, and socat are installed

Options:
  --cluster NAME
  --assets-workdir DIR
  --runtime-root DIR
  --local-repo DIR
  --kubeconfig PATH
  --registry-port PORT
  --release-line NAME
  --channel alpha|beta|stable
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
      --runtime-root)
        RUNTIME_ROOT="${2:?missing value for --runtime-root}"
        shift 2
        ;;
      --local-repo)
        LOCAL_REPO="${2:?missing value for --local-repo}"
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
      --release-line)
        RELEASE_LINE="${2:?missing value for --release-line}"
        shift 2
        ;;
      --channel)
        RELEASE_CHANNEL="${2:?missing value for --channel}"
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
    --workdir "${ASSETS_WORKDIR}/oci" \
    --release-line "${RELEASE_LINE}" \
    --channel "${RELEASE_CHANNEL}" > "${PUBLISH_ENV_FILE}"
}

target_args() {
  printf '%s\n' \
    "--release-source" "${release_source}" \
    "--release-line" "${release_line}" \
    "--channel" "${release_channel}"
}

runtime_root_args() {
  if [[ -n "${RUNTIME_ROOT}" ]]; then
    printf '%s\n' "--runtime-root" "${RUNTIME_ROOT}"
  fi
}

resolve_local_repo() {
  if [[ -n "${LOCAL_REPO}" ]]; then
    return
  fi
  LOCAL_REPO="${ASSETS_WORKDIR}/local-repo"
}

init_local_repo() {
  set -a
  # shellcheck disable=SC1090
  . "${PUBLISH_ENV_FILE}"
  set +a

  resolve_local_repo
  log "initializing local repo for release target"
  local -a target=()
  local -a runtime=()
  mapfile -t target < <(target_args)
  mapfile -t runtime < <(runtime_root_args)
  "${SEALOS_BIN}" sync local-repo init \
    "${runtime[@]}" \
    --cluster "${CLUSTER_NAME}" \
    "${target[@]}" \
    --output-dir "${LOCAL_REPO}" \
    --overwrite >/dev/null
}

copy_input() {
  local src="$1"
  local dst="$2"
  [[ -f "${src}" ]] || fail "required package default input not found: ${src}"
  install -D -m 0644 "${src}" "${dst}"
}

fill_local_repo_inputs() {
  log "filling local repo inputs from released package defaults"
  copy_input \
    "${SCRIPT_DIR}/packages/containerd/files/etc/containerd/config.toml" \
    "${LOCAL_REPO}/inputs/containerd/containerd-config.toml"
  copy_input \
    "${SCRIPT_DIR}/packages/kubernetes/files/etc/kubernetes/kubeadm.yaml" \
    "${LOCAL_REPO}/inputs/kubernetes/kubeadm-cluster-config.yaml"
  copy_input \
    "${SCRIPT_DIR}/packages/cilium/files/values/basic.yaml" \
    "${LOCAL_REPO}/inputs/cilium/cilium-values.yaml"
}

validate_sources() {
  log "validating release target and local repo inputs"
  local -a target=()
  local -a runtime=()
  mapfile -t target < <(target_args)
  mapfile -t runtime < <(runtime_root_args)
  "${SEALOS_BIN}" sync validate \
    "${runtime[@]}" \
    --cluster "${CLUSTER_NAME}" \
    "${target[@]}" \
    --local-repo "${LOCAL_REPO}" >/dev/null
}

render_bundle() {
  log "rendering desired-state bundle from release metadata source"

  local -a render_args=(
    --cluster "${CLUSTER_NAME}" \
    --package-mode release \
    --release-source "${release_source}" \
    --release-line "${release_line}" \
    --channel "${release_channel}" \
    --sealos-bin "${SEALOS_BIN}"
  )
  if [[ -n "${RUNTIME_ROOT}" ]]; then
    render_args+=(--runtime-root "${RUNTIME_ROOT}")
  fi
  render_args+=(--local-repo "${LOCAL_REPO}")

  SEALOS_BIN="${SEALOS_BIN}" "${SCRIPT_DIR}/render.sh" "${render_args[@]}"

  BUNDLE_DIR="$(bundle_dir)"
}

bundle_dir() {
  if [[ -n "${RUNTIME_ROOT}" ]]; then
    printf '%s\n' "${RUNTIME_ROOT}/${CLUSTER_NAME}/distribution/bundles/current"
  else
    printf '%s\n' "${SEALOS_HOME}/${CLUSTER_NAME}/distribution/bundles/current"
  fi
}

apply_bundle() {
  log "applying rendered bundle with sealos sync apply"
  local -a args=(
    sync
    apply
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}"
  )
  if [[ -n "${RUNTIME_ROOT}" ]]; then
    args+=(--runtime-root "${RUNTIME_ROOT}")
  fi
  "${SEALOS_BIN}" "${args[@]}"
}

validate_cluster() {
  log "validating rendered bundle"
  local -a args=(
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}"
  )
  "${SCRIPT_DIR}/validate.sh" "${args[@]}"
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
  init_local_repo
  fill_local_repo_inputs
  validate_sources
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
  printf 'release source: %s\n' "${release_source}"
  printf 'release line: %s\n' "${release_line}"
  printf 'release channel: %s\n' "${release_channel}"
  printf 'bom digest: %s\n' "${bom_digest}"
  printf 'bundle dir: %s\n' "${BUNDLE_DIR}"
}

main "$@"
