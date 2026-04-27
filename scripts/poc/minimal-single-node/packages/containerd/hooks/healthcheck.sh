#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

command -v systemctl >/dev/null 2>&1 || fail "systemctl is required for the PoC runtime healthcheck"
command -v ctr >/dev/null 2>&1 || fail "ctr is required for the PoC runtime healthcheck"

[[ -S /run/containerd/containerd.sock ]] || fail "containerd socket /run/containerd/containerd.sock not found"
systemctl is-active --quiet containerd || fail "containerd service is not active"
ctr version >/dev/null
