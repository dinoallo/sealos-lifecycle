#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="poc-minimal"
BUNDLE_DIR=""
KUBECONFIG_PATH="/etc/kubernetes/admin.conf"
SEALOS_HOME="${SEALOS_HOME:-${HOME}/.sealos}"
SKIP_VALIDATE=0

usage() {
  cat <<'EOF'
Usage:
  install.sh [--cluster NAME] [--bundle-dir DIR] [--kubeconfig PATH] [--skip-validate]

This is a PoC-only installer for the rendered bundle produced by:
  sealos sync render --cluster <name> --package-source ...

The script is intentionally specific to the minimal single-node PoC layout.
It will refuse to run while the package payloads are still placeholders.
EOF
}

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    fail "install.sh must run as root"
  fi
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

require_file() {
  local path="$1"
  [[ -f "${path}" ]] || fail "required file not found: ${path}"
}

require_dir() {
  local path="$1"
  [[ -d "${path}" ]] || fail "required directory not found: ${path}"
}

require_command() {
  local name="$1"
  command -v "${name}" >/dev/null 2>&1 || fail "required command not found: ${name}"
}

assert_no_placeholder_hook() {
  local hook="$1"
  require_file "${hook}"
  if grep -qi "placeholder" "${hook}"; then
    fail "placeholder hook detected at ${hook}; replace PoC placeholder scripts before host install"
  fi
}

validate_real_payloads() {
  local containerd_root="$1"
  local kubernetes_root="$2"
  local cilium_manifest="$3"
  local containerd_hooks_root="$4"
  local kubernetes_hooks_root="$5"
  local cilium_hooks_root="$6"

  local missing=()
  local path
  for path in \
    "${containerd_root}/usr/bin/containerd" \
    "${containerd_root}/usr/bin/ctr" \
    "${containerd_root}/usr/bin/containerd-shim-runc-v2" \
    "${containerd_root}/usr/bin/runc" \
    "${kubernetes_root}/usr/bin/kubeadm" \
    "${kubernetes_root}/usr/bin/kubelet" \
    "${kubernetes_root}/usr/bin/kubectl"; do
    [[ -f "${path}" ]] || missing+=("${path}")
  done
  if ((${#missing[@]} > 0)); then
    printf 'error: the rendered bundle still contains placeholder rootfs content.\n' >&2
    printf 'missing required payloads:\n' >&2
    printf '  %s\n' "${missing[@]}" >&2
    fail "replace the PoC placeholder files with real runtime and Kubernetes binaries before running install.sh"
  fi

  if ! grep -Eq '^kind: DaemonSet$' "${cilium_manifest}" || ! grep -Eq '^kind: Deployment$' "${cilium_manifest}"; then
    fail "Cilium manifest ${cilium_manifest} does not look like a real Cilium install payload"
  fi

  assert_no_placeholder_hook "${containerd_hooks_root}/preflight.sh"
  assert_no_placeholder_hook "${containerd_hooks_root}/bootstrap.sh"
  assert_no_placeholder_hook "${containerd_hooks_root}/healthcheck.sh"
  assert_no_placeholder_hook "${kubernetes_hooks_root}/preflight.sh"
  assert_no_placeholder_hook "${kubernetes_hooks_root}/bootstrap.sh"
  assert_no_placeholder_hook "${kubernetes_hooks_root}/healthcheck.sh"
  assert_no_placeholder_hook "${cilium_hooks_root}/healthcheck.sh"
}

copy_tree() {
  local src="$1"
  local dst="$2"
  require_dir "${src}"
  mkdir -p "${dst}"

  while IFS= read -r -d '' path; do
    local rel="${path#"${src}/"}"
    local target="${dst%/}/${rel}"

    if [[ -d "${path}" ]]; then
      mkdir -p "${target}"
      continue
    fi

    if [[ -L "${path}" ]]; then
      install -d "$(dirname "${target}")"
      rm -f "${target}"
      ln -s "$(readlink "${path}")" "${target}"
      continue
    fi

    if [[ -f "${path}" ]]; then
      local target_dir
      local tmp

      target_dir="$(dirname "${target}")"
      install -d "${target_dir}"
      tmp="$(mktemp "${target}.sealos-tmp.XXXXXX")"
      cp -a "${path}" "${tmp}"
      mv -f "${tmp}" "${target}"
      continue
    fi

    fail "unsupported rootfs entry type: ${path}"
  done < <(find "${src}" -mindepth 1 -print0)
}

copy_file_to_host() {
  local src="$1"
  local dst="$2"
  require_file "${src}"
  install -d "$(dirname "${dst}")"
  cp -a "${src}" "${dst}"
}

run_hook() {
  local hook="$1"
  local component_root="$2"
  local description="$3"
  require_file "${hook}"
  if [[ ! -x "${hook}" ]]; then
    chmod +x "${hook}"
  fi
  log "${description}"
  COMPONENT_ROOT="${component_root}" \
    BUNDLE_DIR="${RESOLVED_BUNDLE_DIR}" \
    CLUSTER_NAME="${CLUSTER_NAME}" \
    KUBECONFIG="${KUBECONFIG_PATH}" \
    "${hook}"
}

reload_systemd() {
  if command -v systemctl >/dev/null 2>&1; then
    log "reloading systemd units"
    systemctl daemon-reload
  fi
}

stop_service_if_active() {
  local service="$1"
  if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet "${service}"; then
    log "stopping ${service} before replacing host binaries"
    systemctl stop "${service}"
  fi
}

apply_sysctl_if_present() {
  local file="$1"
  if [[ -f "${file}" ]] && command -v sysctl >/dev/null 2>&1; then
    log "applying sysctl configuration"
    sysctl --system >/dev/null
  fi
}

wait_for_file() {
  local path="$1"
  local timeout_seconds="$2"
  local waited=0
  while [[ ! -f "${path}" ]]; do
    if (( waited >= timeout_seconds )); then
      fail "timed out waiting for file: ${path}"
    fi
    sleep 1
    waited=$((waited + 1))
  done
}

wait_for_api() {
  local waited=0
  while ! kubectl --kubeconfig "${KUBECONFIG_PATH}" get --raw='/readyz' >/dev/null 2>&1; do
    if (( waited >= 180 )); then
      fail "timed out waiting for Kubernetes API readiness"
    fi
    sleep 2
    waited=$((waited + 2))
  done
}

untaint_single_node_control_plane() {
  local node
  node="$(kubectl --kubeconfig "${KUBECONFIG_PATH}" get nodes -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -z "${node}" ]]; then
    return
  fi

  log "removing control-plane taints from ${node} for single-node scheduling"
  kubectl --kubeconfig "${KUBECONFIG_PATH}" taint nodes "${node}" node-role.kubernetes.io/control-plane- >/dev/null 2>&1 || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" taint nodes "${node}" node-role.kubernetes.io/master- >/dev/null 2>&1 || true
}

main() {
  parse_args "$@"
  require_root

  RESOLVED_BUNDLE_DIR="$(resolve_bundle_dir)"
  readonly RESOLVED_BUNDLE_DIR

  readonly CONTAINERD_COMPONENT_ROOT="${RESOLVED_BUNDLE_DIR}/components/containerd/files"
  readonly KUBERNETES_COMPONENT_ROOT="${RESOLVED_BUNDLE_DIR}/components/kubernetes/files"
  readonly CILIUM_COMPONENT_ROOT="${RESOLVED_BUNDLE_DIR}/components/cilium/files"

  readonly CONTAINERD_ROOTFS="${CONTAINERD_COMPONENT_ROOT}/rootfs"
  readonly CONTAINERD_CONFIG="${CONTAINERD_COMPONENT_ROOT}/files/etc/containerd/config.toml"
  readonly CONTAINERD_HOOKS="${CONTAINERD_COMPONENT_ROOT}/hooks"

  readonly KUBERNETES_ROOTFS="${KUBERNETES_COMPONENT_ROOT}/rootfs"
  readonly KUBERNETES_KUBEADM_CONFIG="${KUBERNETES_COMPONENT_ROOT}/files/etc/kubernetes/kubeadm.yaml"
  readonly KUBERNETES_SYSCTL="${KUBERNETES_COMPONENT_ROOT}/files/etc/sysctl.d/99-kubernetes.conf"
  readonly KUBERNETES_MANIFESTS="${KUBERNETES_COMPONENT_ROOT}/manifests/bootstrap"
  readonly KUBERNETES_HOOKS="${KUBERNETES_COMPONENT_ROOT}/hooks"

  readonly CILIUM_MANIFEST="${CILIUM_COMPONENT_ROOT}/manifests/cilium.yaml"
  readonly CILIUM_VALUES="${CILIUM_COMPONENT_ROOT}/files/values/basic.yaml"
  readonly CILIUM_HOOKS="${CILIUM_COMPONENT_ROOT}/hooks"

  require_file "${RESOLVED_BUNDLE_DIR}/bundle.yaml"
  require_dir "${CONTAINERD_COMPONENT_ROOT}"
  require_dir "${KUBERNETES_COMPONENT_ROOT}"
  require_dir "${CILIUM_COMPONENT_ROOT}"
  require_file "${CONTAINERD_CONFIG}"
  require_file "${KUBERNETES_KUBEADM_CONFIG}"
  require_file "${KUBERNETES_SYSCTL}"
  require_dir "${KUBERNETES_MANIFESTS}"
  require_file "${CILIUM_MANIFEST}"
  require_file "${CILIUM_VALUES}"

  validate_real_payloads \
    "${CONTAINERD_ROOTFS}" \
    "${KUBERNETES_ROOTFS}" \
    "${CILIUM_MANIFEST}" \
    "${CONTAINERD_HOOKS}" \
    "${KUBERNETES_HOOKS}" \
    "${CILIUM_HOOKS}"

  log "using rendered bundle at ${RESOLVED_BUNDLE_DIR}"

  run_hook "${CONTAINERD_HOOKS}/preflight.sh" "${CONTAINERD_COMPONENT_ROOT}" "running containerd preflight hook"
  stop_service_if_active containerd
  copy_tree "${CONTAINERD_ROOTFS}" "/"
  copy_file_to_host "${CONTAINERD_CONFIG}" "/etc/containerd/config.toml"
  reload_systemd
  run_hook "${CONTAINERD_HOOKS}/bootstrap.sh" "${CONTAINERD_COMPONENT_ROOT}" "running containerd bootstrap hook"
  run_hook "${CONTAINERD_HOOKS}/healthcheck.sh" "${CONTAINERD_COMPONENT_ROOT}" "running containerd healthcheck hook"

  stop_service_if_active kubelet
  copy_tree "${KUBERNETES_ROOTFS}" "/"
  copy_file_to_host "${KUBERNETES_KUBEADM_CONFIG}" "/etc/kubernetes/kubeadm.yaml"
  copy_file_to_host "${KUBERNETES_SYSCTL}" "/etc/sysctl.d/99-kubernetes.conf"
  run_hook "${KUBERNETES_HOOKS}/preflight.sh" "${KUBERNETES_COMPONENT_ROOT}" "running kubernetes preflight hook"
  apply_sysctl_if_present "/etc/sysctl.d/99-kubernetes.conf"
  reload_systemd
  run_hook "${KUBERNETES_HOOKS}/bootstrap.sh" "${KUBERNETES_COMPONENT_ROOT}" "running kubernetes bootstrap hook"

  require_command kubectl
  wait_for_file "${KUBECONFIG_PATH}" 180
  export KUBECONFIG="${KUBECONFIG_PATH}"
  wait_for_api
  untaint_single_node_control_plane

  log "applying kubernetes bootstrap manifests"
  kubectl --kubeconfig "${KUBECONFIG_PATH}" apply -f "${KUBERNETES_MANIFESTS}"
  run_hook "${KUBERNETES_HOOKS}/healthcheck.sh" "${KUBERNETES_COMPONENT_ROOT}" "running kubernetes healthcheck hook"

  log "applying cilium manifests"
  kubectl --kubeconfig "${KUBECONFIG_PATH}" apply -f "${CILIUM_MANIFEST}"
  run_hook "${CILIUM_HOOKS}/healthcheck.sh" "${CILIUM_COMPONENT_ROOT}" "running cilium healthcheck hook"

  if (( SKIP_VALIDATE == 0 )); then
    log "running post-install validation"
    "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/validate.sh" \
      --cluster "${CLUSTER_NAME}" \
      --bundle-dir "${RESOLVED_BUNDLE_DIR}" \
      --kubeconfig "${KUBECONFIG_PATH}"
  fi
}

main "$@"
