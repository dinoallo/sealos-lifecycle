#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKDIR="${SCRIPT_DIR}/artifacts"
ARCH="amd64"
KUBERNETES_VERSION="v1.30.3"
CONTAINERD_VERSION="1.7.18"
RUNC_VERSION="1.1.13"
CILIUM_VERSION="1.15.0"
CILIUM_CLI_VERSION="${CILIUM_CLI_VERSION:-}"
CILIUM_VALUES="${SCRIPT_DIR}/packages/cilium/files/values/basic.yaml"
CILIUM_MANIFEST_OUT="${CILIUM_MANIFEST_OUT:-}"

usage() {
  cat <<'EOF'
Usage:
  fetch-assets.sh [options]

Downloads the minimal PoC prerequisites on the current machine so they can be
staged into package directories without copying large binaries from elsewhere.

Options:
  --workdir DIR
  --arch ARCH
  --kubernetes-version VERSION
  --containerd-version VERSION
  --runc-version VERSION
  --cilium-version VERSION
  --cilium-cli-version VERSION
  --cilium-values FILE
  --cilium-manifest-out FILE
EOF
}

log() {
  printf '==> %s\n' "$*" >&2
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workdir)
        WORKDIR="${2:?missing value for --workdir}"
        shift 2
        ;;
      --arch)
        ARCH="${2:?missing value for --arch}"
        shift 2
        ;;
      --kubernetes-version)
        KUBERNETES_VERSION="${2:?missing value for --kubernetes-version}"
        shift 2
        ;;
      --containerd-version)
        CONTAINERD_VERSION="${2:?missing value for --containerd-version}"
        shift 2
        ;;
      --runc-version)
        RUNC_VERSION="${2:?missing value for --runc-version}"
        shift 2
        ;;
      --cilium-version)
        CILIUM_VERSION="${2:?missing value for --cilium-version}"
        shift 2
        ;;
      --cilium-cli-version)
        CILIUM_CLI_VERSION="${2:?missing value for --cilium-cli-version}"
        shift 2
        ;;
      --cilium-values)
        CILIUM_VALUES="${2:?missing value for --cilium-values}"
        shift 2
        ;;
      --cilium-manifest-out)
        CILIUM_MANIFEST_OUT="${2:?missing value for --cilium-manifest-out}"
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

  if [[ -z "${CILIUM_MANIFEST_OUT}" ]]; then
    CILIUM_MANIFEST_OUT="${WORKDIR}/cilium/cilium.yaml"
  fi
}

require_command() {
  local cmd="$1"
  command -v "${cmd}" >/dev/null 2>&1 || fail "required command not found: ${cmd}"
}

require_file() {
  local path="$1"
  [[ -f "${path}" ]] || fail "required file not found: ${path}"
}

download() {
  local url="$1"
  local out="$2"
  log "downloading ${url}"
  curl -fsSL "${url}" -o "${out}"
}

download_kubernetes_binaries() {
  local outdir="${WORKDIR}/kubernetes/${KUBERNETES_VERSION}/bin/linux/${ARCH}"
  install -d "${outdir}"

  local base="https://dl.k8s.io/release/${KUBERNETES_VERSION}/bin/linux/${ARCH}"
  local name
  for name in kubeadm kubelet kubectl; do
    download "${base}/${name}" "${outdir}/${name}"
    chmod +x "${outdir}/${name}"
  done
}

download_containerd() {
  local outdir="${WORKDIR}/containerd/${CONTAINERD_VERSION}"
  local tarball="containerd-${CONTAINERD_VERSION}-linux-${ARCH}.tar.gz"
  install -d "${outdir}"
  download "https://github.com/containerd/containerd/releases/download/v${CONTAINERD_VERSION}/${tarball}" "${outdir}/${tarball}"
  tar -xzf "${outdir}/${tarball}" -C "${outdir}"
}

download_runc() {
  local outdir="${WORKDIR}/runc/${RUNC_VERSION}"
  install -d "${outdir}"
  download "https://github.com/opencontainers/runc/releases/download/v${RUNC_VERSION}/runc.${ARCH}" "${outdir}/runc"
  chmod +x "${outdir}/runc"
}

resolve_cilium_cli_version() {
  if [[ -n "${CILIUM_CLI_VERSION}" ]]; then
    return
  fi
  CILIUM_CLI_VERSION="$(curl -fsSL https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)"
}

download_cilium_cli() {
  resolve_cilium_cli_version

  local outdir="${WORKDIR}/cilium-cli/${CILIUM_CLI_VERSION}"
  local tarball="cilium-linux-${ARCH}.tar.gz"
  install -d "${outdir}"

  download "https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/${tarball}" "${outdir}/${tarball}"
  download "https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/${tarball}.sha256sum" "${outdir}/${tarball}.sha256sum"
  (
    cd "${outdir}"
    sha256sum --quiet --check "${tarball}.sha256sum"
    tar -xzf "${tarball}"
    chmod +x cilium
  )
}

render_cilium_manifest() {
  local cli="${WORKDIR}/cilium-cli/${CILIUM_CLI_VERSION}/cilium"
  require_file "${CILIUM_VALUES}"
  require_file "${cli}"

  install -d "$(dirname "${CILIUM_MANIFEST_OUT}")"
  "${cli}" install \
    --dry-run \
    --version "${CILIUM_VERSION}" \
    -f "${CILIUM_VALUES}" > "${CILIUM_MANIFEST_OUT}"
}

print_summary() {
  cat <<EOF
containerd_bin=${WORKDIR}/containerd/${CONTAINERD_VERSION}/bin/containerd
containerd_shim_bin=${WORKDIR}/containerd/${CONTAINERD_VERSION}/bin/containerd-shim-runc-v2
ctr_bin=${WORKDIR}/containerd/${CONTAINERD_VERSION}/bin/ctr
runc_bin=${WORKDIR}/runc/${RUNC_VERSION}/runc
kubeadm_bin=${WORKDIR}/kubernetes/${KUBERNETES_VERSION}/bin/linux/${ARCH}/kubeadm
kubelet_bin=${WORKDIR}/kubernetes/${KUBERNETES_VERSION}/bin/linux/${ARCH}/kubelet
kubectl_bin=${WORKDIR}/kubernetes/${KUBERNETES_VERSION}/bin/linux/${ARCH}/kubectl
cilium_manifest=${CILIUM_MANIFEST_OUT}
cilium_cli=${WORKDIR}/cilium-cli/${CILIUM_CLI_VERSION}/cilium
EOF
}

main() {
  parse_args "$@"
  require_command curl
  require_command tar
  require_command sha256sum

  download_containerd
  download_runc
  download_kubernetes_binaries
  download_cilium_cli
  render_cilium_manifest
  print_summary
}

main "$@"
