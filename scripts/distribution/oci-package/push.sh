#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
. "${SCRIPT_DIR}/common.sh"

IMAGE=""
DESTINATION=""
AUTHFILE=""
CERT_DIR=""
CREDS=""
DIGEST_FILE=""
digest_file_owned=0

usage() {
  cat <<'EOF'
Usage:
  push.sh --image IMAGE --destination REF [options]

Pushes a previously built package image and prints the resulting image and
digest in a machine-readable form.

If --destination does not include an explicit transport prefix, docker:// is
used automatically.

Options:
  --image IMAGE
  --destination REF
  --authfile PATH
  --cert-dir PATH
  --creds USER[:PASS]
  --digest-file PATH
  --sealos-bin PATH
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --image)
        IMAGE="${2:?missing value for --image}"
        shift 2
        ;;
      --destination)
        DESTINATION="${2:?missing value for --destination}"
        shift 2
        ;;
      --authfile)
        AUTHFILE="${2:?missing value for --authfile}"
        shift 2
        ;;
      --cert-dir)
        CERT_DIR="${2:?missing value for --cert-dir}"
        shift 2
        ;;
      --creds)
        CREDS="${2:?missing value for --creds}"
        shift 2
        ;;
      --digest-file)
        DIGEST_FILE="${2:?missing value for --digest-file}"
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

  [[ -n "${IMAGE}" ]] || fail "--image is required"
  [[ -n "${DESTINATION}" ]] || fail "--destination is required"
}

main() {
  parse_args "$@"
  resolve_sealos_bin

  local destination_ref
  destination_ref="$(normalize_push_destination "${DESTINATION}")"

  if [[ -z "${DIGEST_FILE}" ]]; then
    DIGEST_FILE="$(mktemp)"
    digest_file_owned=1
  fi
  trap "if (( ${digest_file_owned} != 0 )); then rm -f -- \"${DIGEST_FILE}\"; fi" EXIT

  local -a push_args=(
    push
    --digestfile "${DIGEST_FILE}"
  )

  if [[ -n "${AUTHFILE}" ]]; then
    push_args+=(--authfile "${AUTHFILE}")
  fi
  if [[ -n "${CERT_DIR}" ]]; then
    push_args+=(--cert-dir "${CERT_DIR}")
  fi
  if [[ -n "${CREDS}" ]]; then
    push_args+=(--creds "${CREDS}")
  fi

  push_args+=("${IMAGE}" "${destination_ref}")

  log "pushing ${IMAGE} to ${destination_ref}"
  "${SEALOS_BIN}" "${push_args[@]}"

  local digest
  digest="$(<"${DIGEST_FILE}")"
  [[ -n "${digest}" ]] || fail "push completed without digest output"

  local image_ref
  image_ref="$(image_ref_from_destination "${destination_ref}")"

  print_kv source_image "${IMAGE}"
  print_kv destination "${destination_ref}"
  print_kv image "${image_ref}"
  print_kv digest "${digest}"
  print_kv reference "${image_ref}@${digest}"
}

main "$@"
