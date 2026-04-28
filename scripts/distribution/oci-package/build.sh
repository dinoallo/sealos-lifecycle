#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
. "${SCRIPT_DIR}/common.sh"

PACKAGE_DIR=""
IMAGE=""
PLATFORM="linux/amd64"
TIMESTAMP="0"
DISTRIBUTION=""
EXTRA_LABELS=()

usage() {
  cat <<'EOF'
Usage:
  build.sh --package-dir DIR --image IMAGE [options]

Builds a deterministic OCI image from a Sealos component package directory.
The resulting image keeps package.yaml at image root and only includes package
manifest-referenced content.

Options:
  --package-dir DIR
  --image IMAGE
  --platform OS/ARCH
  --timestamp EPOCH
  --distribution NAME
  --label KEY=VALUE
  --sealos-bin PATH
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --package-dir)
        PACKAGE_DIR="${2:?missing value for --package-dir}"
        shift 2
        ;;
      --image)
        IMAGE="${2:?missing value for --image}"
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
      --label)
        EXTRA_LABELS+=("${2:?missing value for --label}")
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

  [[ -n "${PACKAGE_DIR}" ]] || fail "--package-dir is required"
  [[ -n "${IMAGE}" ]] || fail "--image is required"
}

main() {
  parse_args "$@"
  resolve_sealos_bin

  tmpdir="$(mktemp -d)"
  trap "rm -rf -- \"${tmpdir}\"" EXIT

  local package_root
  local PACKAGE_NAME
  local PACKAGE_COMPONENT
  local PACKAGE_VERSION
  local PACKAGE_CLASS

  log "validating package directory ${PACKAGE_DIR}"
  eval "$(load_package_metadata "${PACKAGE_DIR}")"

  local context_dir="${tmpdir}/context"
  local containerfile="${tmpdir}/Containerfile"

  log "staging package build context"
  stage_package_context "${PACKAGE_DIR}" "${context_dir}"
  find "${context_dir}" -exec touch -h -d "@${TIMESTAMP}" {} +

  cat > "${containerfile}" <<'EOF'
FROM scratch
COPY . /
EOF

  local -a build_args=(
    build
    --file "${containerfile}"
    --format oci
    --pull=false
    --save-image=false
    --timestamp "${TIMESTAMP}"
    --platform "${PLATFORM}"
    --tag "${IMAGE}"
    --label "sealos.io.type=${PACKAGE_CLASS}"
    --label "sealos.io.version=${PACKAGE_VERSION}"
    --label "distribution.sealos.io/api-version=v1alpha1"
    --label "distribution.sealos.io/kind=ComponentPackage"
    --label "distribution.sealos.io/package-name=${PACKAGE_NAME}"
    --label "distribution.sealos.io/component=${PACKAGE_COMPONENT}"
    --label "distribution.sealos.io/version=${PACKAGE_VERSION}"
  )

  if [[ -n "${DISTRIBUTION}" ]]; then
    build_args+=(--label "sealos.io.distribution=${DISTRIBUTION}")
  fi

  local label
  for label in "${EXTRA_LABELS[@]}"; do
    build_args+=(--label "${label}")
  done

  build_args+=("${context_dir}")

  log "building ${IMAGE}"
  "${SEALOS_BIN}" "${build_args[@]}"
  "${SEALOS_BIN}" inspect --type image "${IMAGE}" >/dev/null

  print_kv package_dir "${package_root}"
  print_kv image "${IMAGE}"
  print_kv package_name "${PACKAGE_NAME}"
  print_kv package_component "${PACKAGE_COMPONENT}"
  print_kv package_version "${PACKAGE_VERSION}"
  print_kv package_class "${PACKAGE_CLASS}"
  print_kv platform "${PLATFORM}"
}

main "$@"
