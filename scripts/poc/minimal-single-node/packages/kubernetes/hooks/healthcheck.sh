#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required for the PoC kubernetes healthcheck"
KUBECONFIG="${KUBECONFIG:-/etc/kubernetes/admin.conf}"
[[ -f "${KUBECONFIG}" ]] || fail "kubeconfig not found: ${KUBECONFIG}"

waited=0
until kubectl --kubeconfig "${KUBECONFIG}" get --raw='/readyz' >/dev/null 2>&1; do
  if (( waited >= 180 )); then
    fail "timed out waiting for Kubernetes API readiness"
  fi
  sleep 2
  waited=$((waited + 2))
done
