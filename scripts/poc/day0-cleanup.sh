#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="poc-minimal"
RUNTIME_ROOT="${SEALOS_RUNTIME_ROOT:-${HOME}/.sealos}"
DISTRIBUTION_ROOT="/var/lib/sealos/distribution"
LOCAL_REPO=""
WORKDIR=""
SEALOS_BIN="${SEALOS_BIN:-}"
DRY_RUN=0
RESET_CLUSTER=0
YES_RESET=0
REMOTE_STAGED=0

usage() {
  cat <<'EOF'
Usage:
  day0-cleanup.sh [options]

Cleans repeat-run state for the scriptless Day 0 PoC flow. By default this
removes only local distribution render/apply state, local repo content, and an
explicit temporary workdir. It does not remove Kubernetes, CRI, kubelet,
containerd, Clusterfile, admin.conf, or host data.

Options:
  --cluster NAME              Cluster name. Default is poc-minimal.
  --runtime-root DIR          Sealos runtime root. Default is $SEALOS_RUNTIME_ROOT
                              or $HOME/.sealos.
  --distribution-root DIR     Local distribution root that may contain
                              <cluster>/local-repo. Default is
                              /var/lib/sealos/distribution.
  --local-repo DIR            Explicit local repo path to remove.
  --workdir DIR               Temporary PoC workdir to remove.
  --sealos-bin PATH           sealos binary used with --remote-staged or
                              --reset-cluster.
  --dry-run                   Print cleanup actions without deleting anything.
  --remote-staged             Remove staged bundle mirrors from remote hosts
                              using "sealos exec -c <cluster>". This only works
                              when the Clusterfile lives in the default sealos
                              runtime root used by sealos exec.
  --reset-cluster             Run sealos reset --cluster <name> --force before
                              local cleanup. Requires --yes-reset and
                              I_UNDERSTAND_THIS_MUTATES_HOST=1.
  --yes-reset                 Confirm the dangerous reset path.
  -h, --help                  Show this help.
EOF
}

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --cluster)
        CLUSTER_NAME="${2:?missing value for --cluster}"
        shift 2
        ;;
      --runtime-root)
        RUNTIME_ROOT="${2:?missing value for --runtime-root}"
        shift 2
        ;;
      --distribution-root)
        DISTRIBUTION_ROOT="${2:?missing value for --distribution-root}"
        shift 2
        ;;
      --local-repo)
        LOCAL_REPO="${2:?missing value for --local-repo}"
        shift 2
        ;;
      --workdir)
        WORKDIR="${2:?missing value for --workdir}"
        shift 2
        ;;
      --sealos-bin)
        SEALOS_BIN="${2:?missing value for --sealos-bin}"
        shift 2
        ;;
      --dry-run)
        DRY_RUN=1
        shift
        ;;
      --remote-staged)
        REMOTE_STAGED=1
        shift
        ;;
      --reset-cluster)
        RESET_CLUSTER=1
        shift
        ;;
      --yes-reset)
        YES_RESET=1
        shift
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

  [[ -n "${CLUSTER_NAME}" ]] || fail "--cluster cannot be empty"
  [[ -n "${RUNTIME_ROOT}" ]] || fail "--runtime-root cannot be empty"
  [[ -n "${DISTRIBUTION_ROOT}" ]] || fail "--distribution-root cannot be empty"
  case "${CLUSTER_NAME}" in
    "."|".."|*/*)
      fail "--cluster must be a cluster name, not a path: ${CLUSTER_NAME}"
      ;;
  esac
  validate_root_path "--runtime-root" "${RUNTIME_ROOT}"
  validate_root_path "--distribution-root" "${DISTRIBUTION_ROOT}"
  if [[ -n "${LOCAL_REPO}" ]]; then
    validate_root_path "--local-repo" "${LOCAL_REPO}"
  fi
  if [[ -n "${WORKDIR}" ]]; then
    validate_root_path "--workdir" "${WORKDIR}"
  fi
}

abs_path() {
  realpath -m -- "$1"
}

dangerous_path() {
  local path="$1"
  local home_path
  local cwd_path
  home_path="$(abs_path "${HOME}")"
  cwd_path="$(abs_path "${PWD}")"

  case "${path}" in
    ""|"/"|"/var"|"/var/lib"|"/var/lib/sealos"|"/tmp"|"/var/tmp")
      return 0
      ;;
  esac
  [[ "${path}" == "${home_path}" || "${path}" == "${cwd_path}" ]]
}

validate_root_path() {
  local label="$1"
  local raw_path="$2"
  local path
  path="$(abs_path "${raw_path}")"
  if dangerous_path "${path}"; then
    fail "refusing dangerous ${label}: ${path}"
  fi
}

print_cmd() {
  local first=1
  for arg in "$@"; do
    if (( first == 0 )); then
      printf ' '
    fi
    printf '%q' "${arg}"
    first=0
  done
  printf '\n'
}

run_cmd() {
  if (( DRY_RUN != 0 )); then
    printf '+ '
    print_cmd "$@"
    return
  fi
  "$@"
}

remove_path() {
  local label="$1"
  local raw_path="$2"
  [[ -n "${raw_path}" ]] || return 0

  local path
  path="$(abs_path "${raw_path}")"
  if dangerous_path "${path}"; then
    fail "refusing to remove dangerous ${label} path: ${path}"
  fi
  if [[ ! -e "${path}" ]]; then
    log "skip missing ${label}: ${path}"
    return
  fi
  log "remove ${label}: ${path}"
  run_cmd rm -rf -- "${path}"
}

resolve_sealos_bin() {
  if [[ -n "${SEALOS_BIN}" ]]; then
    [[ -x "${SEALOS_BIN}" ]] || fail "sealos binary is not executable: ${SEALOS_BIN}"
    printf '%s\n' "${SEALOS_BIN}"
    return
  fi
  if command -v sealos >/dev/null 2>&1; then
    command -v sealos
    return
  fi
  if [[ -x "./bin/linux_amd64/sealos" ]]; then
    printf '%s\n' "./bin/linux_amd64/sealos"
    return
  fi
  fail "sealos binary not found; pass --sealos-bin for --remote-staged or --reset-cluster"
}

reset_cluster_if_requested() {
  if (( RESET_CLUSTER == 0 && REMOTE_STAGED == 0 )); then
    return
  fi
  if (( RESET_CLUSTER != 0 )); then
    if (( YES_RESET == 0 )); then
      fail "--reset-cluster requires --yes-reset"
    fi
    if [[ "${I_UNDERSTAND_THIS_MUTATES_HOST:-}" != "1" ]]; then
      fail "--reset-cluster requires I_UNDERSTAND_THIS_MUTATES_HOST=1"
    fi
  fi

  local sealos_bin
  sealos_bin="$(resolve_sealos_bin)"

  if (( REMOTE_STAGED != 0 )); then
    local remote_staged_path
    remote_staged_path="$(abs_path "${RUNTIME_ROOT}/${CLUSTER_NAME}/distribution/bundles/staged")"
    log "remove remote staged bundle mirrors: ${remote_staged_path}"
    run_cmd "${sealos_bin}" exec -c "${CLUSTER_NAME}" "rm -rf -- $(printf '%q' "${remote_staged_path}")"
  fi

  if (( RESET_CLUSTER == 0 )); then
    return
  fi
  log "reset cluster ${CLUSTER_NAME}"
  run_cmd "${sealos_bin}" reset --cluster "${CLUSTER_NAME}" --force
}

cleanup_local_state() {
  local runtime_cluster_root
  runtime_cluster_root="$(abs_path "${RUNTIME_ROOT}/${CLUSTER_NAME}")"

  remove_path "rendered distribution state" "${runtime_cluster_root}/distribution"

  if [[ -n "${LOCAL_REPO}" ]]; then
    remove_path "local repo" "${LOCAL_REPO}"
  else
    remove_path "runtime local repo" "${runtime_cluster_root}/local-repo"
    remove_path "distribution local repo" "${DISTRIBUTION_ROOT}/${CLUSTER_NAME}/local-repo"
  fi

  if [[ -n "${WORKDIR}" ]]; then
    remove_path "temporary PoC workdir" "${WORKDIR}"
  fi
}

main() {
  parse_args "$@"

  log "cluster: ${CLUSTER_NAME}"
  log "runtime root: $(abs_path "${RUNTIME_ROOT}")"
  reset_cluster_if_requested
  cleanup_local_state
  log "Day 0 cleanup completed"
}

main "$@"
