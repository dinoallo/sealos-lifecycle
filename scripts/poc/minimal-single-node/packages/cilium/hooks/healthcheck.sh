#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required for the PoC cilium healthcheck"
KUBECONFIG="${KUBECONFIG:-/etc/kubernetes/admin.conf}"
[[ -f "${KUBECONFIG}" ]] || fail "kubeconfig not found: ${KUBECONFIG}"

kubectl --kubeconfig "${KUBECONFIG}" -n kube-system rollout status daemonset/cilium --timeout=180s
kubectl --kubeconfig "${KUBECONFIG}" -n kube-system rollout status deployment/cilium-operator --timeout=180s
