#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[[ "${EUID}" -eq 0 ]] || fail "kubernetes bootstrap must run as root"
command -v kubeadm >/dev/null 2>&1 || fail "kubeadm is required for bootstrap"
command -v systemctl >/dev/null 2>&1 || fail "systemctl is required for bootstrap"
[[ -f /etc/kubernetes/kubeadm.yaml ]] || fail "/etc/kubernetes/kubeadm.yaml not found"

systemctl enable kubelet >/dev/null
systemctl daemon-reload
systemctl restart kubelet || true

if [[ "${TARGET_IS_FIRST_MASTER:-false}" == "true" ]]; then
  if [[ -f /etc/kubernetes/admin.conf ]]; then
    exit 0
  fi

  kubeadm init \
    --config /etc/kubernetes/kubeadm.yaml \
    --skip-token-print \
    --ignore-preflight-errors=SystemVerification,FileExisting-crictl,CRI
else
  if [[ -f /etc/kubernetes/kubelet.conf ]]; then
    exit 0
  fi

  kubeadm join \
    --config /etc/kubernetes/kubeadm.yaml \
    --ignore-preflight-errors=SystemVerification,FileExisting-crictl,CRI
fi

if [[ -f /etc/kubernetes/admin.conf ]]; then
  install -d -m 0700 /root/.kube
  cp -f /etc/kubernetes/admin.conf /root/.kube/config
  chmod 0600 /root/.kube/config
fi
