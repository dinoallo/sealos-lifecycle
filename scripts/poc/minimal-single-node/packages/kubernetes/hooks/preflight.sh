#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[[ "${EUID}" -eq 0 ]] || fail "kubernetes preflight must run as root"

for cmd in kubeadm kubelet kubectl systemctl modprobe swapon; do
  command -v "${cmd}" >/dev/null 2>&1 || fail "required command not found: ${cmd}"
done

[[ -S /run/containerd/containerd.sock ]] || fail "containerd socket /run/containerd/containerd.sock not found"

if swapon --noheadings --show | grep -q .; then
  fail "swap must be disabled before kubeadm init"
fi

modprobe overlay >/dev/null 2>&1 || true
modprobe br_netfilter >/dev/null 2>&1 || true
