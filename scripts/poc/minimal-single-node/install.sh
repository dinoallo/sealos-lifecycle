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

This wrapper still validates that the rendered bundle contains real staged
assets before delegating host mutation to `sealos sync apply`.
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

require_file() {
  local path="$1"
  [[ -f "${path}" ]] || fail "required file not found: ${path}"
}

require_dir() {
  local path="$1"
  [[ -d "${path}" ]] || fail "required directory not found: ${path}"
}

assert_no_placeholder_hook() {
  local hook="$1"
  require_file "${hook}"
  if grep -qi "placeholder" "${hook}"; then
    fail "placeholder hook detected at ${hook}; replace PoC placeholder scripts before host apply"
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
    fail "replace the PoC placeholder files with real runtime and Kubernetes binaries before host apply"
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

main() {
  parse_args "$@"
  require_root
  resolve_sealos_bin

  RESOLVED_BUNDLE_DIR="$(resolve_bundle_dir)"
  readonly RESOLVED_BUNDLE_DIR

  readonly CONTAINERD_COMPONENT_ROOT="${RESOLVED_BUNDLE_DIR}/components/containerd/files"
  readonly KUBERNETES_COMPONENT_ROOT="${RESOLVED_BUNDLE_DIR}/components/kubernetes/files"
  readonly CILIUM_COMPONENT_ROOT="${RESOLVED_BUNDLE_DIR}/components/cilium/files"

  readonly CONTAINERD_ROOTFS="${CONTAINERD_COMPONENT_ROOT}/rootfs"
  readonly CONTAINERD_HOOKS="${CONTAINERD_COMPONENT_ROOT}/hooks"

  readonly KUBERNETES_ROOTFS="${KUBERNETES_COMPONENT_ROOT}/rootfs"
  readonly KUBERNETES_HOOKS="${KUBERNETES_COMPONENT_ROOT}/hooks"

  readonly CILIUM_MANIFEST="${CILIUM_COMPONENT_ROOT}/manifests/cilium.yaml"
  readonly CILIUM_HOOKS="${CILIUM_COMPONENT_ROOT}/hooks"

  require_file "${RESOLVED_BUNDLE_DIR}/bundle.yaml"
  require_dir "${CONTAINERD_COMPONENT_ROOT}"
  require_dir "${KUBERNETES_COMPONENT_ROOT}"
  require_dir "${CILIUM_COMPONENT_ROOT}"
  require_file "${CILIUM_MANIFEST}"

  validate_real_payloads \
    "${CONTAINERD_ROOTFS}" \
    "${KUBERNETES_ROOTFS}" \
    "${CILIUM_MANIFEST}" \
    "${CONTAINERD_HOOKS}" \
    "${KUBERNETES_HOOKS}" \
    "${CILIUM_HOOKS}"

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
