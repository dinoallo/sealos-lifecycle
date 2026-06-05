#!/usr/bin/env bash
set -euo pipefail

POC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${POC_DIR}/../../.." && pwd)"
OCI_HELPER_DIR="${REPO_ROOT}/scripts/distribution/oci-package"
# shellcheck source=../../distribution/oci-package/common.sh
. "${OCI_HELPER_DIR}/common.sh"

WORKDIR="${POC_DIR}/artifacts/oci"
REGISTRY_PREFIX=""
PACKAGE_ROOT=""
BOM_OUT=""
RELEASE_ROOT=""
PLATFORM="linux/amd64"
TIMESTAMP="0"
DISTRIBUTION="poc-minimal"
RELEASE_LINE="minimal-single-node"
RELEASE_CHANNEL="alpha"
RELEASE_REVISION="rev-poc-001"

CONTAINERD_PACKAGE_NAME=""
CONTAINERD_PACKAGE_VERSION=""
CONTAINERD_IMAGE=""
CONTAINERD_DIGEST=""

KUBERNETES_PACKAGE_NAME=""
KUBERNETES_PACKAGE_VERSION=""
KUBERNETES_IMAGE=""
KUBERNETES_DIGEST=""

CILIUM_PACKAGE_NAME=""
CILIUM_PACKAGE_VERSION=""
CILIUM_IMAGE=""
CILIUM_DIGEST=""

BOM_DIGEST=""
RELEASE_BOM_PATH=""
RELEASE_CHANNEL_PATH=""

usage() {
  cat <<'EOF'
Usage:
  publish-oci.sh --registry-prefix REF [options]

Stages the minimal single-node PoC package set into a temporary package root,
builds OCI package images for all three components, pushes them, and writes a
BOM whose artifact references point at the pushed images.

Examples:
  publish-oci.sh --registry-prefix localhost:5055/poc-minimal

Options:
  --registry-prefix REF
  --workdir DIR
  --package-root DIR
  --bom-out PATH
  --release-root DIR
  --platform OS/ARCH
  --timestamp EPOCH
  --distribution NAME
  --release-line NAME
  --channel alpha|beta|stable
  --revision REVISION
  --sealos-bin PATH
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --registry-prefix)
        REGISTRY_PREFIX="${2:?missing value for --registry-prefix}"
        shift 2
        ;;
      --workdir)
        WORKDIR="${2:?missing value for --workdir}"
        shift 2
        ;;
      --package-root)
        PACKAGE_ROOT="${2:?missing value for --package-root}"
        shift 2
        ;;
      --bom-out)
        BOM_OUT="${2:?missing value for --bom-out}"
        shift 2
        ;;
      --release-root)
        RELEASE_ROOT="${2:?missing value for --release-root}"
        shift 2
        ;;
      --platform)
        PLATFORM="${2:?missing value for --platform}"
        shift 2
        ;;
      --timestamp)
        TIMESTAMP="${2:?missing value for --timestamp}"
        shift 2
        ;;
      --distribution)
        DISTRIBUTION="${2:?missing value for --distribution}"
        shift 2
        ;;
      --release-line)
        RELEASE_LINE="${2:?missing value for --release-line}"
        shift 2
        ;;
      --channel)
        RELEASE_CHANNEL="${2:?missing value for --channel}"
        shift 2
        ;;
      --revision)
        RELEASE_REVISION="${2:?missing value for --revision}"
        shift 2
        ;;
      --sealos-bin)
        SEALOS_BIN="${2:?missing value for --sealos-bin}"
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

  [[ -n "${REGISTRY_PREFIX}" ]] || fail "--registry-prefix is required"
}

resolve_paths() {
  install -d "${WORKDIR}"

  if [[ -z "${PACKAGE_ROOT}" ]]; then
    PACKAGE_ROOT="$(mktemp -d "${WORKDIR}/packages.XXXXXX")"
  elif [[ -e "${PACKAGE_ROOT}" ]]; then
    if find "${PACKAGE_ROOT}" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
      fail "package root must be empty when it already exists: ${PACKAGE_ROOT}"
    fi
  else
    install -d "${PACKAGE_ROOT}"
  fi

  if [[ -z "${BOM_OUT}" ]]; then
    BOM_OUT="${WORKDIR}/bom.oci.yaml"
  fi

  if [[ -z "${RELEASE_ROOT}" ]]; then
    RELEASE_ROOT="${WORKDIR}/release"
  fi
}

stage_temp_package_root() {
  local assets_env_file
  assets_env_file="$(mktemp "${WORKDIR}/fetch-assets.XXXXXX.env")"

  log "copying package templates into ${PACKAGE_ROOT}"
  cp -a "${POC_DIR}/packages/." "${PACKAGE_ROOT}/"

  log "fetching package assets"
  "${POC_DIR}/fetch-assets.sh" --workdir "${WORKDIR}/downloads" > "${assets_env_file}"
  # shellcheck disable=SC1090
  source "${assets_env_file}"

  log "staging assets into ${PACKAGE_ROOT}"
  "${POC_DIR}/stage-assets.sh" \
    --package-root "${PACKAGE_ROOT}" \
    --containerd-bin "${containerd_bin}" \
    --containerd-shim-bin "${containerd_shim_bin}" \
    --ctr-bin "${ctr_bin}" \
    --runc-bin "${runc_bin}" \
    --kubeadm-bin "${kubeadm_bin}" \
    --kubelet-bin "${kubelet_bin}" \
    --kubectl-bin "${kubectl_bin}" \
    --cilium-manifest "${cilium_manifest}" >&2

  rm -f -- "${assets_env_file}"
}

build_and_push_component() {
  local component="$1"
  local package_dir="${PACKAGE_ROOT}/${component}"
  local image=""

  eval "$("${SEALOS_BIN}" sync package inspect --package-dir "${package_dir}" --output env)"
  image="${REGISTRY_PREFIX}/${package_name}:${package_version}"

  log "building package image for ${component}"
  "${OCI_HELPER_DIR}/build.sh" \
    --package-dir "${package_dir}" \
    --image "${image}" \
    --platform "${PLATFORM}" \
    --timestamp "${TIMESTAMP}" \
    --distribution "${DISTRIBUTION}" >/dev/null

  log "pushing package image for ${component}"
  eval "$(
    "${OCI_HELPER_DIR}/push.sh" \
      --image "${image}" \
      --destination "${image}"
  )"

  case "${component}" in
    containerd)
      CONTAINERD_PACKAGE_NAME="${package_name}"
      CONTAINERD_PACKAGE_VERSION="${package_version}"
      CONTAINERD_IMAGE="${image}"
      CONTAINERD_DIGEST="${digest}"
      ;;
    kubernetes)
      KUBERNETES_PACKAGE_NAME="${package_name}"
      KUBERNETES_PACKAGE_VERSION="${package_version}"
      KUBERNETES_IMAGE="${image}"
      KUBERNETES_DIGEST="${digest}"
      ;;
    cilium)
      CILIUM_PACKAGE_NAME="${package_name}"
      CILIUM_PACKAGE_VERSION="${package_version}"
      CILIUM_IMAGE="${image}"
      CILIUM_DIGEST="${digest}"
      ;;
    *)
      fail "unsupported component: ${component}"
      ;;
  esac
}

write_bom() {
  install -d "$(dirname "${BOM_OUT}")"

  cat > "${BOM_OUT}" <<EOF
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: ${RELEASE_LINE}
  labels:
    distribution.sealos.io/example: "true"
    distribution.sealos.io/profile: poc
spec:
  revision: ${RELEASE_REVISION}
  channel: ${RELEASE_CHANNEL}
  packages:
    - name: containerd
      category: infra
      version: ${CONTAINERD_PACKAGE_VERSION}
      artifact:
        name: ${CONTAINERD_PACKAGE_NAME}
        image: ${CONTAINERD_IMAGE}
        digest: ${CONTAINERD_DIGEST}
    - name: kubernetes
      category: infra
      version: ${KUBERNETES_PACKAGE_VERSION}
      dependencies:
        - containerd
      artifact:
        name: ${KUBERNETES_PACKAGE_NAME}
        image: ${KUBERNETES_IMAGE}
        digest: ${KUBERNETES_DIGEST}
    - name: cilium
      category: infra
      version: ${CILIUM_PACKAGE_VERSION}
      dependencies:
        - kubernetes
      artifact:
        name: ${CILIUM_PACKAGE_NAME}
        image: ${CILIUM_IMAGE}
        digest: ${CILIUM_DIGEST}
EOF
}

write_release_metadata() {
  RELEASE_BOM_PATH="${RELEASE_ROOT}/releases/${RELEASE_LINE}/${RELEASE_REVISION}/bom.yaml"
  RELEASE_CHANNEL_PATH="${RELEASE_ROOT}/channels/${RELEASE_LINE}/${RELEASE_CHANNEL}.yaml"

  install -d "$(dirname "${RELEASE_BOM_PATH}")" "$(dirname "${RELEASE_CHANNEL_PATH}")"
  cp "${BOM_OUT}" "${RELEASE_BOM_PATH}"

  BOM_DIGEST="sha256:$(sha256sum "${RELEASE_BOM_PATH}" | awk '{print $1}')"

  cat > "${RELEASE_CHANNEL_PATH}" <<EOF
apiVersion: distribution.sealos.io/v1alpha1
kind: ReleaseChannel
metadata:
  name: ${RELEASE_LINE}-${RELEASE_CHANNEL}
spec:
  distribution: ${RELEASE_LINE}
  channel: ${RELEASE_CHANNEL}
  targetRevision: ${RELEASE_REVISION}
  bomPath: ../../releases/${RELEASE_LINE}/${RELEASE_REVISION}/bom.yaml
  bomDigest: ${BOM_DIGEST}
EOF
}

print_summary() {
  print_kv workdir "${WORKDIR}"
  print_kv package_root "${PACKAGE_ROOT}"
  print_kv bom_path "${BOM_OUT}"
  print_kv bom_digest "${BOM_DIGEST}"
  print_kv release_source "${RELEASE_ROOT}"
  print_kv release_line "${RELEASE_LINE}"
  print_kv release_channel "${RELEASE_CHANNEL}"
  print_kv release_revision "${RELEASE_REVISION}"
  print_kv release_bom_path "${RELEASE_BOM_PATH}"
  print_kv release_channel_path "${RELEASE_CHANNEL_PATH}"
  print_kv containerd_reference "${CONTAINERD_IMAGE}@${CONTAINERD_DIGEST}"
  print_kv kubernetes_reference "${KUBERNETES_IMAGE}@${KUBERNETES_DIGEST}"
  print_kv cilium_reference "${CILIUM_IMAGE}@${CILIUM_DIGEST}"
}

main() {
  parse_args "$@"
  resolve_paths
  resolve_sealos_bin

  stage_temp_package_root
  build_and_push_component containerd
  build_and_push_component kubernetes
  build_and_push_component cilium
  write_bom
  write_release_metadata
  print_summary
}

main "$@"
