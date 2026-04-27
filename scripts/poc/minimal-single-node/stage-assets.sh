#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_ROOT="${SCRIPT_DIR}/packages"

CONTAINERD_BIN="${CONTAINERD_BIN:-}"
CONTAINERD_SHIM_BIN="${CONTAINERD_SHIM_BIN:-}"
CTR_BIN="${CTR_BIN:-}"
RUNC_BIN="${RUNC_BIN:-}"
KUBEADM_BIN="${KUBEADM_BIN:-}"
KUBELET_BIN="${KUBELET_BIN:-}"
KUBECTL_BIN="${KUBECTL_BIN:-}"
CILIUM_MANIFEST="${CILIUM_MANIFEST:-}"
CILIUM_VALUES="${CILIUM_VALUES:-}"
CILIUM_CHART_DIR="${CILIUM_CHART_DIR:-}"
HELM_BIN="${HELM_BIN:-}"

usage() {
  cat <<'EOF'
Usage:
  stage-assets.sh [options]

Copies local runtime/Kubernetes artifacts into the PoC package directories so
the rendered bundle can move beyond placeholder payloads.

Required inputs:
  --kubelet-bin PATH

One of:
  --cilium-manifest PATH
  --cilium-chart-dir PATH --helm-bin PATH

Optional inputs:
  --package-root PATH
  --containerd-bin PATH
  --containerd-shim-bin PATH
  --ctr-bin PATH
  --runc-bin PATH
  --kubeadm-bin PATH
  --kubectl-bin PATH
  --cilium-values PATH

Defaults:
  --containerd-bin: command -v containerd
  --containerd-shim-bin: command -v containerd-shim-runc-v2
  --ctr-bin:        command -v ctr
  --runc-bin:       command -v runc
  --kubectl-bin:    command -v kubectl
  --kubeadm-bin:    command -v kubeadm
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
      --package-root)
        PACKAGE_ROOT="${2:?missing value for --package-root}"
        shift 2
        ;;
      --containerd-bin)
        CONTAINERD_BIN="${2:?missing value for --containerd-bin}"
        shift 2
        ;;
      --containerd-shim-bin)
        CONTAINERD_SHIM_BIN="${2:?missing value for --containerd-shim-bin}"
        shift 2
        ;;
      --ctr-bin)
        CTR_BIN="${2:?missing value for --ctr-bin}"
        shift 2
        ;;
      --runc-bin)
        RUNC_BIN="${2:?missing value for --runc-bin}"
        shift 2
        ;;
      --kubeadm-bin)
        KUBEADM_BIN="${2:?missing value for --kubeadm-bin}"
        shift 2
        ;;
      --kubelet-bin)
        KUBELET_BIN="${2:?missing value for --kubelet-bin}"
        shift 2
        ;;
      --kubectl-bin)
        KUBECTL_BIN="${2:?missing value for --kubectl-bin}"
        shift 2
        ;;
      --cilium-manifest)
        CILIUM_MANIFEST="${2:?missing value for --cilium-manifest}"
        shift 2
        ;;
      --cilium-values)
        CILIUM_VALUES="${2:?missing value for --cilium-values}"
        shift 2
        ;;
      --cilium-chart-dir)
        CILIUM_CHART_DIR="${2:?missing value for --cilium-chart-dir}"
        shift 2
        ;;
      --helm-bin)
        HELM_BIN="${2:?missing value for --helm-bin}"
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

resolve_default_binaries() {
  [[ -n "${CONTAINERD_BIN}" ]] || CONTAINERD_BIN="$(command -v containerd || true)"
  [[ -n "${CONTAINERD_SHIM_BIN}" ]] || CONTAINERD_SHIM_BIN="$(command -v containerd-shim-runc-v2 || true)"
  [[ -n "${CTR_BIN}" ]] || CTR_BIN="$(command -v ctr || true)"
  [[ -n "${RUNC_BIN}" ]] || RUNC_BIN="$(command -v runc || true)"
  [[ -n "${KUBECTL_BIN}" ]] || KUBECTL_BIN="$(command -v kubectl || true)"

  [[ -n "${KUBEADM_BIN}" ]] || KUBEADM_BIN="$(command -v kubeadm || true)"
}

require_file() {
  local path="$1"
  [[ -f "${path}" ]] || fail "required file not found: ${path}"
}

require_executable() {
  local path="$1"
  require_file "${path}"
  [[ -x "${path}" ]] || fail "required executable is not executable: ${path}"
}

require_dir() {
  local path="$1"
  [[ -d "${path}" ]] || fail "required directory not found: ${path}"
}

copy_binary() {
  local src="$1"
  local dst="$2"
  require_executable "${src}"
  install -D -m 0755 "${src}" "${dst}"
}

copy_file() {
  local src="$1"
  local dst="$2"
  require_file "${src}"
  install -D -m 0644 "${src}" "${dst}"
}

render_cilium_chart() {
  local chart_dir="$1"
  local helm_bin="$2"
  local values_src="$3"
  local manifest_dst="$4"

  require_dir "${chart_dir}"
  require_executable "${helm_bin}"
  require_file "${values_src}"
  install -d "$(dirname "${manifest_dst}")"
  "${helm_bin}" template cilium "${chart_dir}" \
    --namespace kube-system \
    -f "${values_src}" > "${manifest_dst}"
}

validate_args() {
  require_dir "${PACKAGE_ROOT}"

  [[ -n "${CONTAINERD_BIN}" ]] || fail "containerd binary not found; pass --containerd-bin"
  [[ -n "${CONTAINERD_SHIM_BIN}" ]] || fail "containerd-shim-runc-v2 binary not found; pass --containerd-shim-bin"
  [[ -n "${CTR_BIN}" ]] || fail "ctr binary not found; pass --ctr-bin"
  [[ -n "${RUNC_BIN}" ]] || fail "runc binary not found; pass --runc-bin"
  [[ -n "${KUBEADM_BIN}" ]] || fail "kubeadm binary not found; pass --kubeadm-bin"
  [[ -n "${KUBELET_BIN}" ]] || fail "kubelet binary not found; pass --kubelet-bin"
  [[ -n "${KUBECTL_BIN}" ]] || fail "kubectl binary not found; pass --kubectl-bin"

  if [[ -z "${CILIUM_MANIFEST}" ]]; then
    [[ -n "${CILIUM_CHART_DIR}" ]] || fail "pass --cilium-manifest or --cilium-chart-dir with --helm-bin"
    [[ -n "${HELM_BIN}" ]] || fail "pass --helm-bin when using --cilium-chart-dir"
  fi
}

main() {
  parse_args "$@"
  resolve_default_binaries
  validate_args

  local containerd_rootfs="${PACKAGE_ROOT}/containerd/rootfs"
  local kubernetes_rootfs="${PACKAGE_ROOT}/kubernetes/rootfs"
  local cilium_manifest_dst="${PACKAGE_ROOT}/cilium/manifests/cilium.yaml"
  local cilium_values_dst="${PACKAGE_ROOT}/cilium/files/values/basic.yaml"

  log "staging runtime binaries"
  copy_binary "${CONTAINERD_BIN}" "${containerd_rootfs}/usr/bin/containerd"
  copy_binary "${CONTAINERD_SHIM_BIN}" "${containerd_rootfs}/usr/bin/containerd-shim-runc-v2"
  copy_binary "${CTR_BIN}" "${containerd_rootfs}/usr/bin/ctr"
  copy_binary "${RUNC_BIN}" "${containerd_rootfs}/usr/bin/runc"

  log "staging kubernetes binaries"
  copy_binary "${KUBEADM_BIN}" "${kubernetes_rootfs}/usr/bin/kubeadm"
  copy_binary "${KUBELET_BIN}" "${kubernetes_rootfs}/usr/bin/kubelet"
  copy_binary "${KUBECTL_BIN}" "${kubernetes_rootfs}/usr/bin/kubectl"

  if [[ -n "${CILIUM_VALUES}" ]]; then
    log "staging cilium values"
    copy_file "${CILIUM_VALUES}" "${cilium_values_dst}"
  fi

  if [[ -n "${CILIUM_MANIFEST}" ]]; then
    log "staging cilium manifest"
    copy_file "${CILIUM_MANIFEST}" "${cilium_manifest_dst}"
  else
    log "rendering cilium chart"
    render_cilium_chart "${CILIUM_CHART_DIR}" "${HELM_BIN}" "${cilium_values_dst}" "${cilium_manifest_dst}"
  fi

  log "asset staging completed"
  printf 'package root: %s\n' "${PACKAGE_ROOT}"
}

main "$@"
