#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
SEALOS_BIN="${SEALOS_BIN:-}"

log() {
  printf '==> %s\n' "$*" >&2
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  local cmd="$1"
  command -v "${cmd}" >/dev/null 2>&1 || fail "required command not found: ${cmd}"
}

print_kv() {
  printf '%s=%q\n' "$1" "$2"
}

resolve_sealos_bin() {
  if [[ -n "${SEALOS_BIN}" ]]; then
    [[ -x "${SEALOS_BIN}" ]] || fail "sealos binary is not executable: ${SEALOS_BIN}"
    return
  fi

  if [[ -x "${REPO_ROOT}/bin/linux_amd64/sealos" ]]; then
    SEALOS_BIN="${REPO_ROOT}/bin/linux_amd64/sealos"
    return
  fi

  if command -v sealos >/dev/null 2>&1; then
    SEALOS_BIN="$(command -v sealos)"
    return
  fi

  fail "sealos binary not found; build it first or pass --sealos-bin"
}

load_package_metadata() {
  local package_dir="$1"

  require_command go
  [[ -d "${package_dir}" ]] || fail "package directory not found: ${package_dir}"

  (
    cd "${REPO_ROOT}"
    go run ./scripts/distribution/oci-package/metadata \
      --package-dir "${package_dir}" \
      --format env
  )
}

package_paths() {
  local package_dir="$1"

  require_command go
  [[ -d "${package_dir}" ]] || fail "package directory not found: ${package_dir}"

  (
    cd "${REPO_ROOT}"
    go run ./scripts/distribution/oci-package/metadata \
      --package-dir "${package_dir}" \
      --format path-list
  )
}

stage_package_context() {
  local package_dir="$1"
  local context_dir="$2"

  install -d "${context_dir}"

  while IFS= read -r rel_path; do
    local src_path="${package_dir}/${rel_path}"
    local dst_path="${context_dir}/${rel_path}"

    install -d "$(dirname "${dst_path}")"
    cp -a "${src_path}" "${dst_path}"
  done < <(package_paths "${package_dir}")
}

normalize_push_destination() {
  local destination="$1"

  case "${destination}" in
    containers-storage:*|dir:*|docker://*|docker-archive:*|docker-daemon:*|oci:*|oci-archive:*|ostree:*|sif:*)
      printf '%s\n' "${destination}"
      ;;
    *)
      printf 'docker://%s\n' "${destination}"
      ;;
  esac
}

image_ref_from_destination() {
  local destination="$1"

  case "${destination}" in
    docker://*)
      printf '%s\n' "${destination#docker://}"
      ;;
    *)
      printf '%s\n' "${destination}"
      ;;
  esac
}
