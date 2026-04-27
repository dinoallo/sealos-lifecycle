#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[[ "${EUID}" -eq 0 ]] || fail "containerd bootstrap must run as root"
command -v systemctl >/dev/null 2>&1 || fail "systemctl is required for the PoC runtime bootstrap"
[[ -f /etc/containerd/config.toml ]] || fail "/etc/containerd/config.toml not found"

systemctl enable containerd >/dev/null
systemctl restart containerd
