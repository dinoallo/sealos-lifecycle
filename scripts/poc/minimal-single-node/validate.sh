#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="poc-minimal"
KUBECONFIG_PATH="/etc/kubernetes/admin.conf"
WAIT_TIMEOUT="180s"
SMOKE_IMAGE="busybox:1.36"
SEALOS_HOME="${SEALOS_HOME:-${HOME}/.sealos}"
BUNDLE_DIR=""
SMOKE_POD_NAME="smoke-poc"

usage() {
  cat <<'EOF'
Usage:
  validate.sh [--cluster NAME] [--bundle-dir DIR] [--kubeconfig PATH] [--wait-timeout DURATION] [--smoke-image IMAGE]

This is a PoC-only validation script for the minimal single-node bundle.
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
      --wait-timeout)
        WAIT_TIMEOUT="${2:?missing value for --wait-timeout}"
        shift 2
        ;;
      --smoke-image)
        SMOKE_IMAGE="${2:?missing value for --smoke-image}"
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

resolve_bundle_dir() {
  if [[ -n "${BUNDLE_DIR}" ]]; then
    printf '%s\n' "${BUNDLE_DIR}"
    return
  fi
  printf '%s\n' "${SEALOS_HOME}/${CLUSTER_NAME}/distribution/bundles/current"
}

k() {
  kubectl --kubeconfig "${KUBECONFIG_PATH}" "$@"
}

cleanup() {
  if command -v kubectl >/dev/null 2>&1 && [[ -f "${KUBECONFIG_PATH}" ]]; then
    k delete pod "${SMOKE_POD_NAME}" --ignore-not-found >/dev/null 2>&1 || true
  fi
}

main() {
  parse_args "$@"
  trap cleanup EXIT

  command -v kubectl >/dev/null 2>&1 || fail "required command not found: kubectl"
  [[ -f "${KUBECONFIG_PATH}" ]] || fail "kubeconfig not found: ${KUBECONFIG_PATH}"

  RESOLVED_BUNDLE_DIR="$(resolve_bundle_dir)"
  readonly RESOLVED_BUNDLE_DIR

  log "using rendered bundle at ${RESOLVED_BUNDLE_DIR}"
  [[ -f "${RESOLVED_BUNDLE_DIR}/bundle.yaml" ]] || fail "bundle manifest not found: ${RESOLVED_BUNDLE_DIR}/bundle.yaml"

  log "checking API readiness"
  k get --raw='/readyz' >/dev/null

  log "checking node readiness"
  k get nodes
  k wait --for=condition=Ready nodes --all --timeout="${WAIT_TIMEOUT}"

  log "checking core system workloads"
  k -n kube-system rollout status deployment/coredns --timeout="${WAIT_TIMEOUT}"
  k -n kube-system rollout status daemonset/cilium --timeout="${WAIT_TIMEOUT}"
  k -n kube-system rollout status deployment/cilium-operator --timeout="${WAIT_TIMEOUT}"

  log "checking cluster pods"
  k get pods -A

  log "running smoke pod"
  k delete pod "${SMOKE_POD_NAME}" --ignore-not-found >/dev/null 2>&1 || true
  k run "${SMOKE_POD_NAME}" --image="${SMOKE_IMAGE}" --restart=Never --command -- sleep 30
  k wait --for=condition=Ready "pod/${SMOKE_POD_NAME}" --timeout="${WAIT_TIMEOUT}"
}

main "$@"
