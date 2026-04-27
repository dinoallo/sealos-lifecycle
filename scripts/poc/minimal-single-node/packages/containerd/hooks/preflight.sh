#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[[ "${EUID}" -eq 0 ]] || fail "containerd preflight must run as root"
command -v systemctl >/dev/null 2>&1 || fail "systemctl is required for the PoC runtime bootstrap"
command -v modprobe >/dev/null 2>&1 || fail "modprobe is required for the PoC runtime bootstrap"

modprobe overlay >/dev/null 2>&1 || true

mkdir -p /var/lib/containerd /run/containerd
