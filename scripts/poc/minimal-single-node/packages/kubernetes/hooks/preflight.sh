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

ensure_tmp_permissions() {
  local tmp_mode
  tmp_mode="$(stat -c '%a' /tmp 2>/dev/null || true)"
  if [[ "${tmp_mode}" != "1777" ]]; then
    chmod 1777 /tmp || fail "failed to set /tmp permissions to 1777 for apt"
  fi
}

fallback_ubuntu_archive_mirror() {
  local sources_file=/etc/apt/sources.list
  [[ -f "${sources_file}" ]] || return 1
  grep -q 'http://cn\.archive\.ubuntu\.com/ubuntu' "${sources_file}" || return 1
  cp "${sources_file}" "${sources_file}.sealos-preflight.bak" || return 1
  sed -i 's#http://cn\\.archive\\.ubuntu\\.com/ubuntu#http://archive.ubuntu.com/ubuntu#g' "${sources_file}" || return 1
  return 0
}

apt_update_indexes() {
  ensure_tmp_permissions
  export DEBIAN_FRONTEND=noninteractive
  if timeout 90s apt-get -o Acquire::ForceIPv4=true update -y >/dev/null; then
    return 0
  fi
  if fallback_ubuntu_archive_mirror; then
    timeout 90s apt-get -o Acquire::ForceIPv4=true update -y >/dev/null \
      || fail "timed out refreshing apt indexes after falling back to archive.ubuntu.com"
    return 0
  fi
  fail "timed out refreshing apt indexes while installing conntrack"
}

apt_install_conntrack() {
  apt_update_indexes
  if ! timeout 120s apt-get -o Acquire::ForceIPv4=true install -y conntrack >/dev/null; then
    command -v conntrack >/dev/null 2>&1 && return 0
    fail "timed out installing conntrack with apt-get"
  fi
}

if ! command -v conntrack >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then
    apt_install_conntrack
  fi
  command -v conntrack >/dev/null 2>&1 || fail "required command not found: conntrack"
fi

[[ -S /run/containerd/containerd.sock ]] || fail "containerd socket /run/containerd/containerd.sock not found"

if swapon --noheadings --show | grep -q .; then
  swapoff -a || fail "failed to disable swap with swapoff -a"
  if [[ -f /etc/fstab ]]; then
    sed -ri '/^[[:space:]]*#/! s@^([^[:space:]].*[[:space:]]swap[[:space:]].*)$@# \\1@' /etc/fstab
  fi
  swapon --noheadings --show | grep -q . && fail "swap must be disabled before kubeadm bootstrap"
fi

modprobe overlay >/dev/null 2>&1 || true
modprobe br_netfilter >/dev/null 2>&1 || true
