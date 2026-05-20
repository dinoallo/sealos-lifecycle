#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
POC_DIR="${REPO_ROOT}/scripts/poc/minimal-single-node"

CLUSTER_NAME="poc-minimal"
SEALOS_BIN="${SEALOS_BIN:-}"
BOM_FILE="${POC_DIR}/bom.yaml"
PACKAGE_MODE="local"
WORKDIR="${WORKDIR:-}"
RUNTIME_ROOT="${RUNTIME_ROOT:-}"
LOCAL_REPO=""
KUBECONFIG_PATH="/etc/kubernetes/admin.conf"
HOST_ROOT="/"
OUTPUT_FORMAT="yaml"
APPLY=0
REVERT_CHECK=0
BUILD_PACKAGES=0
SKIP_BUILD=0
IMAGE_PREFIX="local/poc-smoke"
REPORT_FILE="${REPORT_FILE:-}"
RUN_STARTED_AT=""
REPORT_WRITTEN=0
REPORT_CHECKED=0
FINAL_EXIT_CODE=0

declare -a STAGE_NAMES=()
declare -A STAGE_SEEN=()
declare -A STAGE_STATUS=()
declare -A STAGE_COMMAND=()
declare -A STAGE_OUTPUT=()
declare -A STAGE_REASON=()
declare -A STAGE_STARTED_AT=()
declare -A STAGE_FINISHED_AT=()
declare -A STAGE_MUTATES=()

CONTAINERD_PACKAGE="${POC_DIR}/packages/containerd"
KUBERNETES_PACKAGE="${POC_DIR}/packages/kubernetes"
CILIUM_PACKAGE="${POC_DIR}/packages/cilium"
DEFAULT_LOCAL_BOM="${POC_DIR}/bom.yaml"
DEFAULT_OCI_BOM="${POC_DIR}/artifacts/oci/bom.oci.yaml"
BUNDLE_DIR=""

usage() {
  cat <<'EOF'
Usage:
  smoke.sh [options]

Runs the minimal single-node package lifecycle smoke flow:
  1. inspect local package directories
  2. initialize and fill a temporary cluster-local repo
  3. run local-repo doctor
  4. run source preflight and validate
  5. render the bundle
  6. run rendered bundle runtime preflight
  7. run sync plan and verify sourcePreflight metadata

The default path is safe: it does not run sync apply and writes runtime state
under a temporary workdir. Pass --apply explicitly to mutate the host and then
run sync status/diff against the live cluster. Pass --revert-check with --apply
to inject a temporary Cilium ConfigMap drift and verify object-scoped sync
revert restores the rendered desired state.

Options:
  --cluster NAME
  --sealos-bin PATH
  --bom-file PATH
  --package-mode local|oci
  --workdir DIR
  --runtime-root DIR
  --local-repo DIR
  --skip-build
  --build-packages
  --image-prefix PREFIX
  --report-file PATH
  --apply
  --revert-check
  --kubeconfig PATH
  --host-root PATH
EOF
}

log() {
  printf '==> %s\n' "$*"
}

fail() {
  FINAL_EXIT_CODE=1
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
      --sealos-bin)
        SEALOS_BIN="${2:?missing value for --sealos-bin}"
        shift 2
        ;;
      --bom-file)
        BOM_FILE="${2:?missing value for --bom-file}"
        shift 2
        ;;
      --package-mode)
        PACKAGE_MODE="${2:?missing value for --package-mode}"
        shift 2
        ;;
      --workdir)
        WORKDIR="${2:?missing value for --workdir}"
        shift 2
        ;;
      --runtime-root)
        RUNTIME_ROOT="${2:?missing value for --runtime-root}"
        shift 2
        ;;
      --local-repo)
        LOCAL_REPO="${2:?missing value for --local-repo}"
        shift 2
        ;;
      --skip-build)
        SKIP_BUILD=1
        shift
        ;;
      --build-packages)
        BUILD_PACKAGES=1
        shift
        ;;
      --image-prefix)
        IMAGE_PREFIX="${2:?missing value for --image-prefix}"
        shift 2
        ;;
      --report-file)
        REPORT_FILE="${2:?missing value for --report-file}"
        shift 2
        ;;
      --apply)
        APPLY=1
        shift
        ;;
      --revert-check)
        REVERT_CHECK=1
        shift
        ;;
      --kubeconfig)
        KUBECONFIG_PATH="${2:?missing value for --kubeconfig}"
        shift 2
        ;;
      --host-root)
        HOST_ROOT="${2:?missing value for --host-root}"
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

  if (( REVERT_CHECK != 0 && APPLY == 0 )); then
    fail "--revert-check requires --apply because it mutates live Kubernetes state"
  fi
}

now_utc() {
  date -u '+%Y-%m-%dT%H:%M:%SZ'
}

bool_yaml() {
  if (( "$1" != 0 )); then
    printf 'true'
  else
    printf 'false'
  fi
}

yaml_string() {
  local value="${1:-}"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  printf '"%s"' "${value}"
}

format_command() {
  local out=""
  local quoted
  for arg in "$@"; do
    printf -v quoted '%q' "${arg}"
    out+=" ${quoted}"
  done
  printf '%s' "${out# }"
}

begin_stage() {
  local name="$1"
  local mutates="${2:-0}"

  if [[ -z "${STAGE_SEEN[${name}]:-}" ]]; then
    STAGE_NAMES+=("${name}")
    STAGE_SEEN["${name}"]=1
  fi
  STAGE_STATUS["${name}"]="Running"
  STAGE_REASON["${name}"]=""
  STAGE_STARTED_AT["${name}"]="$(now_utc)"
  STAGE_FINISHED_AT["${name}"]=""
  STAGE_MUTATES["${name}"]="$(bool_yaml "${mutates}")"
}

complete_stage() {
  local name="$1"
  local status="$2"
  local reason="${3:-}"

  STAGE_STATUS["${name}"]="${status}"
  STAGE_REASON["${name}"]="${reason}"
  STAGE_FINISHED_AT["${name}"]="$(now_utc)"
}

skip_stage() {
  local name="$1"
  local reason="$2"
  local mutates="${3:-0}"

  begin_stage "${name}" "${mutates}"
  complete_stage "${name}" "Skipped" "${reason}"
}

fail_stage() {
  local name="$1"
  shift
  local message="$*"

  if [[ -n "${STAGE_SEEN[${name}]:-}" ]]; then
    complete_stage "${name}" "Failed" "${message}"
  fi
  fail "${message}"
}

build_sealos() {
  begin_stage "build-sealos"
  STAGE_COMMAND["build-sealos"]="make build BINS=sealos"
  if (( SKIP_BUILD != 0 )); then
    complete_stage "build-sealos" "Skipped" "pass without --skip-build to build the current sealos binary"
    return
  fi

  log "build-sealos"
  command -v make >/dev/null 2>&1 || fail_stage "build-sealos" "required command not found: make"
  command -v go >/dev/null 2>&1 || fail_stage "build-sealos" "required command not found: go"
  if ! (
    cd "${REPO_ROOT}"
    make build BINS=sealos
  ); then
    fail_stage "build-sealos" "make build BINS=sealos failed"
  fi
  SEALOS_BIN="${REPO_ROOT}/bin/linux_amd64/sealos"
  complete_stage "build-sealos" "Passed"
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

check_sealos_support() {
  local help
  help="$("${SEALOS_BIN}" sync --help 2>&1 || true)"
  grep -q 'local-repo' <<<"${help}" ||
    fail "sealos binary does not list 'sync local-repo'; rebuild current source or omit --skip-build"
  grep -q 'preflight' <<<"${help}" ||
    fail "sealos binary does not list 'sync preflight'; rebuild current source or omit --skip-build"
  grep -q 'plan' <<<"${help}" ||
    fail "sealos binary does not list 'sync plan'; rebuild current source or omit --skip-build"
  grep -q 'validate' <<<"${help}" ||
    fail "sealos binary does not list 'sync validate'; rebuild current source or omit --skip-build"
  if (( REVERT_CHECK != 0 )); then
    grep -q 'revert' <<<"${help}" ||
      fail "sealos binary does not list 'sync revert'; rebuild current source or omit --skip-build"
  fi
}

init_paths() {
  if [[ -z "${WORKDIR}" ]]; then
    WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/sealos-poc-smoke.XXXXXX")"
  fi
  mkdir -p "${WORKDIR}"

  if [[ -z "${RUNTIME_ROOT}" ]]; then
    RUNTIME_ROOT="${WORKDIR}/runtime"
  fi
  mkdir -p "${RUNTIME_ROOT}"

  if [[ -z "${LOCAL_REPO}" ]]; then
    LOCAL_REPO="${WORKDIR}/local-repo"
  fi
  mkdir -p "${LOCAL_REPO}"
}

resolve_package_mode() {
  case "${PACKAGE_MODE}" in
    local)
      ;;
    oci)
      if [[ "${BOM_FILE}" == "${DEFAULT_LOCAL_BOM}" ]]; then
        BOM_FILE="${DEFAULT_OCI_BOM}"
      fi
      ;;
    *)
      fail "unsupported package mode: ${PACKAGE_MODE} (want local or oci)"
      ;;
  esac

  [[ -f "${BOM_FILE}" ]] || fail "bom file not found: ${BOM_FILE}"
}

package_source_args() {
  if [[ "${PACKAGE_MODE}" != "local" ]]; then
    return
  fi
  printf '%s\n' \
    "--package-source" "containerd=${CONTAINERD_PACKAGE}" \
    "--package-source" "kubernetes=${KUBERNETES_PACKAGE}" \
    "--package-source" "cilium=${CILIUM_PACKAGE}"
}

print_command() {
  printf '+'
  for arg in "$@"; do
    printf ' %q' "${arg}"
  done
  printf '\n'
}

run_to_file() {
  local name="$1"
  shift
  local mutates=0
  if [[ "${1:-}" == "--mutates" ]]; then
    mutates=1
    shift
  fi
  local out="${WORKDIR}/${name}.${OUTPUT_FORMAT}"

  log "${name}"
  print_command "$@"
  begin_stage "${name}" "${mutates}"
  STAGE_COMMAND["${name}"]="$(format_command "$@")"
  STAGE_OUTPUT["${name}"]="${out}"
  if "$@" | tee "${out}"; then
    complete_stage "${name}" "Passed"
  else
    local code=$?
    complete_stage "${name}" "Failed" "command exited with status ${code}"
    return "${code}"
  fi
}

kubectl_cmd() {
  kubectl --kubeconfig "${KUBECONFIG_PATH}" "$@"
}

assert_current_state() {
  local name="$1"
  local expected="$2"
  local file="${WORKDIR}/${name}.${OUTPUT_FORMAT}"
  local got

  [[ -f "${file}" ]] || fail_stage "${name}" "output file not found for ${name}: ${file}"
  got="$(awk -F': *' '$1 == "currentState" {gsub(/"/, "", $2); print $2; exit}' "${file}")"
  [[ "${got}" == "${expected}" ]] ||
    fail_stage "${name}" "${name} currentState = ${got:-<missing>}, want ${expected}"
}

assert_not_current_state() {
  local name="$1"
  local unexpected="$2"
  local file="${WORKDIR}/${name}.${OUTPUT_FORMAT}"
  local got

  [[ -f "${file}" ]] || fail_stage "${name}" "output file not found for ${name}: ${file}"
  got="$(awk -F': *' '$1 == "currentState" {gsub(/"/, "", $2); print $2; exit}' "${file}")"
  [[ -n "${got}" ]] || fail_stage "${name}" "${name} currentState is missing"
  [[ "${got}" != "${unexpected}" ]] ||
    fail_stage "${name}" "${name} currentState = ${got}, want a drifted state"
}

stage_summary_status() {
  if (( FINAL_EXIT_CODE != 0 )); then
    printf 'Failed'
    return
  fi

  local failed=0
  local running=0
  for name in "${STAGE_NAMES[@]}"; do
    case "${STAGE_STATUS[${name}]:-Unknown}" in
      Failed)
        failed=1
        ;;
      Running)
        running=1
        ;;
    esac
  done

  if (( failed != 0 )); then
    printf 'Failed'
  elif (( running != 0 )); then
    printf 'Incomplete'
  else
    printf 'Passed'
  fi
}

extract_yaml_value() {
  local file="$1"
  local key="$2"
  [[ -f "${file}" ]] || return
  awk -v key="${key}" '
    index($0, key ":") == 1 {
      line = $0
      sub("^[^:]*:[[:space:]]*", "", line)
      gsub(/^"/, "", line)
      gsub(/"$/, "", line)
      print line
      exit
    }
  ' "${file}"
}

extract_yaml_value_any_indent() {
  local file="$1"
  local key="$2"
  [[ -f "${file}" ]] || return
  awk -v key="${key}" '
    $0 ~ "^[[:space:]]*" key ":" {
      line = $0
      sub("^[[:space:]]*[^:]*:[[:space:]]*", "", line)
      gsub(/^"/, "", line)
      gsub(/"$/, "", line)
      print line
      exit
    }
  ' "${file}"
}

report_path() {
  if [[ -n "${REPORT_FILE}" ]]; then
    printf '%s\n' "${REPORT_FILE}"
  else
    printf '%s\n' "${WORKDIR}/acceptance-report.yaml"
  fi
}

write_acceptance_report() {
  if [[ -z "${WORKDIR}" && -z "${REPORT_FILE}" ]]; then
    return
  fi

  local report
  report="$(report_path)"
  mkdir -p "$(dirname "${report}")"

  local status
  status="$(stage_summary_status)"
  local finished_at
  finished_at="$(now_utc)"
  local render_out="${WORKDIR}/render.${OUTPUT_FORMAT}"
  local source_preflight_out="${WORKDIR}/source-preflight.${OUTPUT_FORMAT}"
  local validate_out="${WORKDIR}/validate.${OUTPUT_FORMAT}"
  local runtime_preflight_out="${WORKDIR}/runtime-preflight.${OUTPUT_FORMAT}"
  local diff_out="${WORKDIR}/diff.${OUTPUT_FORMAT}"
  local revert_diff_out="${WORKDIR}/revert-check-clean-diff.${OUTPUT_FORMAT}"

  {
    printf 'apiVersion: distribution.sealos.io/v1alpha1\n'
    printf 'kind: PackageAcceptanceReport\n'
    printf 'metadata:\n'
    printf '  name: %s\n' "$(yaml_string "${CLUSTER_NAME}")"
    printf 'spec:\n'
    printf '  clusterName: %s\n' "$(yaml_string "${CLUSTER_NAME}")"
    printf '  startedAt: %s\n' "$(yaml_string "${RUN_STARTED_AT}")"
    printf '  finishedAt: %s\n' "$(yaml_string "${finished_at}")"
    printf '  status: %s\n' "$(yaml_string "${status}")"
    printf '  exitCode: %d\n' "${FINAL_EXIT_CODE}"
    printf '  mutatingApply: %s\n' "$(bool_yaml "${APPLY}")"
    printf '  revertCheck: %s\n' "$(bool_yaml "${REVERT_CHECK}")"
    printf '  packageMode: %s\n' "$(yaml_string "${PACKAGE_MODE}")"
    printf '  bomFile: %s\n' "$(yaml_string "${BOM_FILE}")"
    printf '  bomName: %s\n' "$(yaml_string "$(extract_yaml_value "${render_out}" "bomName")")"
    printf '  bomRevision: %s\n' "$(yaml_string "$(extract_yaml_value "${render_out}" "revision")")"
    printf '  bomDigest: %s\n' "$(yaml_string "$(extract_yaml_value_any_indent "${render_out}" "bomDigest")")"
    printf '  workdir: %s\n' "$(yaml_string "${WORKDIR}")"
    printf '  runtimeRoot: %s\n' "$(yaml_string "${RUNTIME_ROOT}")"
    printf '  localRepo: %s\n' "$(yaml_string "${LOCAL_REPO}")"
    printf '  bundleDir: %s\n' "$(yaml_string "${BUNDLE_DIR}")"
    printf '  kubeconfig: %s\n' "$(yaml_string "${KUBECONFIG_PATH}")"
    printf '  hostRoot: %s\n' "$(yaml_string "${HOST_ROOT}")"
    printf '  outputsFormat: %s\n' "$(yaml_string "${OUTPUT_FORMAT}")"
    printf '  desiredStateDigest: %s\n' "$(yaml_string "$(extract_yaml_value "${render_out}" "desiredStateDigest")")"
    printf '  localRepoRevision: %s\n' "$(yaml_string "$(extract_yaml_value "${validate_out}" "localRepoRevision")")"
    printf '  sourcePreflightState: %s\n' "$(yaml_string "$(extract_yaml_value "${source_preflight_out}" "state")")"
    printf '  runtimePreflightState: %s\n' "$(yaml_string "$(extract_yaml_value "${runtime_preflight_out}" "state")")"
    printf '  postApplyState: %s\n' "$(yaml_string "$(extract_yaml_value "${diff_out}" "currentState")")"
    printf '  postRevertState: %s\n' "$(yaml_string "$(extract_yaml_value "${revert_diff_out}" "currentState")")"
    printf '  packageSources:\n'
    printf '  - component: containerd\n'
    printf '    path: %s\n' "$(yaml_string "${CONTAINERD_PACKAGE}")"
    printf '  - component: kubernetes\n'
    printf '    path: %s\n' "$(yaml_string "${KUBERNETES_PACKAGE}")"
    printf '  - component: cilium\n'
    printf '    path: %s\n' "$(yaml_string "${CILIUM_PACKAGE}")"
    printf '  stages:\n'
    for name in "${STAGE_NAMES[@]}"; do
      printf '  - name: %s\n' "$(yaml_string "${name}")"
      printf '    status: %s\n' "$(yaml_string "${STAGE_STATUS[${name}]:-Unknown}")"
      printf '    mutates: %s\n' "${STAGE_MUTATES[${name}]:-false}"
      printf '    startedAt: %s\n' "$(yaml_string "${STAGE_STARTED_AT[${name}]:-}")"
      printf '    finishedAt: %s\n' "$(yaml_string "${STAGE_FINISHED_AT[${name}]:-}")"
      if [[ -n "${STAGE_OUTPUT[${name}]:-}" ]]; then
        printf '    output: %s\n' "$(yaml_string "${STAGE_OUTPUT[${name}]}")"
      fi
      if [[ -n "${STAGE_COMMAND[${name}]:-}" ]]; then
        printf '    command: %s\n' "$(yaml_string "${STAGE_COMMAND[${name}]}")"
      fi
      if [[ -n "${STAGE_REASON[${name}]:-}" ]]; then
        printf '    reason: %s\n' "$(yaml_string "${STAGE_REASON[${name}]}")"
      fi
    done
    printf '  notes:\n'
    printf '  - %s\n' "$(yaml_string "Secret values are not captured in this report.")"
    printf '  - %s\n' "$(yaml_string "The revert check validates drift recovery against rendered desired state; it is not an uninstall or data deletion test.")"
  } >"${report}"

  REPORT_WRITTEN=1
}

write_acceptance_report_on_exit() {
  local code=$?
  FINAL_EXIT_CODE="${code}"
  if (( REPORT_WRITTEN == 0 )); then
    write_acceptance_report || true
  fi
  exit "${code}"
}

acceptance_mode() {
  if (( REVERT_CHECK != 0 )); then
    printf 'revert'
  elif (( APPLY != 0 )); then
    printf 'apply'
  else
    printf 'safe'
  fi
}

check_acceptance_report() {
  local mode
  mode="$(acceptance_mode)"
  log "check-acceptance-report"
  "${POC_DIR}/check-report.sh" --report-file "$(report_path)" --mode "${mode}"
  REPORT_CHECKED=1
}

inspect_packages() {
  run_to_file "package-inspect-containerd" "${SEALOS_BIN}" sync package inspect \
    --package-dir "${CONTAINERD_PACKAGE}" \
    --output "${OUTPUT_FORMAT}"
  run_to_file "package-inspect-kubernetes" "${SEALOS_BIN}" sync package inspect \
    --package-dir "${KUBERNETES_PACKAGE}" \
    --output "${OUTPUT_FORMAT}"
  run_to_file "package-inspect-cilium" "${SEALOS_BIN}" sync package inspect \
    --package-dir "${CILIUM_PACKAGE}" \
    --output "${OUTPUT_FORMAT}"
}

build_packages() {
  if (( BUILD_PACKAGES == 0 )); then
    log "skipping package build; pass --build-packages to exercise OCI image build"
    skip_stage "package-build-containerd" "pass --build-packages to exercise OCI image build"
    skip_stage "package-build-kubernetes" "pass --build-packages to exercise OCI image build"
    skip_stage "package-build-cilium" "pass --build-packages to exercise OCI image build"
    return
  fi

  run_to_file "package-build-containerd" "${SEALOS_BIN}" sync package build \
    --package-dir "${CONTAINERD_PACKAGE}" \
    --image "${IMAGE_PREFIX}/containerd-runtime:v1.7.18" \
    --distribution "${CLUSTER_NAME}" \
    --output "${OUTPUT_FORMAT}"
  run_to_file "package-build-kubernetes" "${SEALOS_BIN}" sync package build \
    --package-dir "${KUBERNETES_PACKAGE}" \
    --image "${IMAGE_PREFIX}/kubernetes-rootfs:v1.30.3" \
    --distribution "${CLUSTER_NAME}" \
    --output "${OUTPUT_FORMAT}"
  run_to_file "package-build-cilium" "${SEALOS_BIN}" sync package build \
    --package-dir "${CILIUM_PACKAGE}" \
    --image "${IMAGE_PREFIX}/cilium-cni:v1.15.0" \
    --distribution "${CLUSTER_NAME}" \
    --output "${OUTPUT_FORMAT}"
}

init_local_repo() {
  local -a sources=()
  mapfile -t sources < <(package_source_args)

  run_to_file "local-repo-init" "${SEALOS_BIN}" sync local-repo init \
    --file "${BOM_FILE}" \
    --output-dir "${LOCAL_REPO}" \
    --overwrite \
    "${sources[@]}" \
    --output "${OUTPUT_FORMAT}"
}

copy_input() {
  local src="$1"
  local dst="$2"
  [[ -f "${src}" ]] || fail "required package default input not found: ${src}"
  install -D -m 0644 "${src}" "${dst}"
}

fill_local_repo_inputs() {
  log "fill-local-repo-inputs"
  begin_stage "fill-local-repo-inputs"
  if ! copy_input \
    "${CONTAINERD_PACKAGE}/files/etc/containerd/config.toml" \
    "${LOCAL_REPO}/inputs/containerd/containerd-config.toml"; then
    fail_stage "fill-local-repo-inputs" "copy containerd default input failed"
  fi
  if ! copy_input \
    "${KUBERNETES_PACKAGE}/files/etc/kubernetes/kubeadm.yaml" \
    "${LOCAL_REPO}/inputs/kubernetes/kubeadm-cluster-config.yaml"; then
    fail_stage "fill-local-repo-inputs" "copy kubernetes default input failed"
  fi
  if ! copy_input \
    "${CILIUM_PACKAGE}/files/values/basic.yaml" \
    "${LOCAL_REPO}/inputs/cilium/cilium-values.yaml"; then
    fail_stage "fill-local-repo-inputs" "copy cilium default input failed"
  fi
  complete_stage "fill-local-repo-inputs" "Passed"
}

doctor_local_repo() {
  local -a sources=()
  mapfile -t sources < <(package_source_args)

  run_to_file "local-repo-doctor" "${SEALOS_BIN}" sync local-repo doctor \
    --file "${BOM_FILE}" \
    --local-repo "${LOCAL_REPO}" \
    "${sources[@]}" \
    --output "${OUTPUT_FORMAT}"
}

validate_sources() {
  local -a sources=()
  mapfile -t sources < <(package_source_args)

  run_to_file "validate" "${SEALOS_BIN}" sync validate \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --file "${BOM_FILE}" \
    --local-repo "${LOCAL_REPO}" \
    "${sources[@]}" \
    --output "${OUTPUT_FORMAT}"
}

source_preflight() {
  local -a sources=()
  mapfile -t sources < <(package_source_args)

  run_to_file "source-preflight" "${SEALOS_BIN}" sync preflight \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --file "${BOM_FILE}" \
    --local-repo "${LOCAL_REPO}" \
    "${sources[@]}" \
    --output "${OUTPUT_FORMAT}"
}

render_bundle() {
  local -a sources=()
  mapfile -t sources < <(package_source_args)

  run_to_file "render" "${SEALOS_BIN}" sync render \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --file "${BOM_FILE}" \
    --local-repo "${LOCAL_REPO}" \
    "${sources[@]}" \
    --output "${OUTPUT_FORMAT}"

  BUNDLE_DIR="$(awk -F': ' '$1 == "bundlePath" {print $2; exit}' "${WORKDIR}/render.${OUTPUT_FORMAT}" | tr -d '"')"
  if [[ -z "${BUNDLE_DIR}" ]]; then
    BUNDLE_DIR="${RUNTIME_ROOT}/${CLUSTER_NAME}/distribution/bundles/current"
  fi
  [[ -f "${BUNDLE_DIR}/bundle.yaml" ]] ||
    fail_stage "render" "rendered bundle manifest not found: ${BUNDLE_DIR}/bundle.yaml"
}

verify_bundle_source_preflight() {
  log "verify-sourcePreflight-metadata"
  begin_stage "verify-sourcePreflight-metadata"
  STAGE_OUTPUT["verify-sourcePreflight-metadata"]="${BUNDLE_DIR}/bundle.yaml"
  grep -q 'sourcePreflight:' "${BUNDLE_DIR}/bundle.yaml" ||
    fail_stage "verify-sourcePreflight-metadata" "rendered bundle is missing spec.sourcePreflight"
  if awk '/sourcePreflight:/,/executionTopology:/' "${BUNDLE_DIR}/bundle.yaml" | grep -q 'blocked: true'; then
    fail_stage "verify-sourcePreflight-metadata" "rendered bundle sourcePreflight is blocked"
  fi
  complete_stage "verify-sourcePreflight-metadata" "Passed"
}

plan_bundle() {
  run_to_file "plan" "${SEALOS_BIN}" sync plan \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --output "${OUTPUT_FORMAT}"

  if grep -q 'missing sourcePreflight metadata' "${WORKDIR}/plan.${OUTPUT_FORMAT}"; then
    fail_stage "plan" "plan reported missing sourcePreflight metadata for a freshly rendered bundle"
  fi
}

runtime_preflight() {
  local runtime_host_root="${HOST_ROOT}"
  if (( APPLY == 0 )); then
    runtime_host_root="${WORKDIR}/host-root"
  fi

  run_to_file "runtime-preflight" "${SEALOS_BIN}" sync preflight \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}" \
    --host-root "${runtime_host_root}" \
    --output "${OUTPUT_FORMAT}"

  grep -q 'runtimeStatus:' "${WORKDIR}/runtime-preflight.${OUTPUT_FORMAT}" ||
    fail_stage "runtime-preflight" "runtime preflight output is missing runtimeStatus"
  if grep -q 'blocked: true' "${WORKDIR}/runtime-preflight.${OUTPUT_FORMAT}"; then
    fail_stage "runtime-preflight" "runtime preflight reported a blocking condition"
  fi
}

revert_check() {
  if (( REVERT_CHECK == 0 )); then
    skip_stage "revert-check-drift-inject" "pass --apply --revert-check to inject temporary drift" 1
    skip_stage "revert-check-drift-diff" "pass --apply --revert-check to inspect temporary drift"
    skip_stage "revert-check-revert" "pass --apply --revert-check to restore temporary drift" 1
    skip_stage "revert-check-clean-diff" "pass --apply --revert-check to verify reverted state"
    skip_stage "validate-cluster-after-revert" "pass --apply --revert-check to validate after revert" 1
    return
  fi

  command -v kubectl >/dev/null 2>&1 || fail "required command not found: kubectl"
  assert_current_state "diff" "Clean"

  run_to_file "revert-check-drift-inject" --mutates kubectl_cmd \
    -n kube-system patch configmap cilium-config \
    --type merge \
    -p '{"data":{"debug":"true"}}'
  run_to_file "revert-check-drift-diff" "${SEALOS_BIN}" sync diff \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}" \
    --host-root "${HOST_ROOT}"
  assert_not_current_state "revert-check-drift-diff" "Clean"
  grep -q 'cilium-config' "${WORKDIR}/revert-check-drift-diff.${OUTPUT_FORMAT}" ||
    fail_stage "revert-check-drift-diff" "revert-check drift diff did not include cilium-config"
  grep -q 'data.debug' "${WORKDIR}/revert-check-drift-diff.${OUTPUT_FORMAT}" ||
    fail_stage "revert-check-drift-diff" "revert-check drift diff did not include data.debug"

  run_to_file "revert-check-revert" --mutates "${SEALOS_BIN}" sync revert \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}" \
    --host-root "${HOST_ROOT}" \
    --api-version v1 \
    --kind ConfigMap \
    --namespace kube-system \
    --name cilium-config
  grep -q '^reverted: true$' "${WORKDIR}/revert-check-revert.${OUTPUT_FORMAT}" ||
    fail_stage "revert-check-revert" "revert-check revert did not report reverted: true"
  assert_current_state "revert-check-revert" "Clean"

  run_to_file "revert-check-clean-diff" "${SEALOS_BIN}" sync diff \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}" \
    --host-root "${HOST_ROOT}"
  assert_current_state "revert-check-clean-diff" "Clean"

  local debug_value
  debug_value="$(kubectl_cmd -n kube-system get configmap cilium-config -o jsonpath='{.data.debug}')"
  [[ "${debug_value}" == "false" ]] ||
    fail_stage "revert-check-clean-diff" "cilium-config data.debug = ${debug_value:-<missing>}, want false after sync revert"

  run_to_file "validate-cluster-after-revert" --mutates "${POC_DIR}/validate.sh" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}"
}

apply_and_observe() {
  if (( APPLY == 0 )); then
    log "skipping apply/status/diff/revert-check; pass --apply to mutate the host and observe live drift"
    skip_stage "apply" "pass --apply to mutate the host and observe live drift" 1
    skip_stage "status" "pass --apply to observe live cluster state"
    skip_stage "diff" "pass --apply to observe live cluster drift"
    skip_stage "validate-cluster" "pass --apply to validate the live cluster" 1
    skip_stage "revert-check-drift-inject" "pass --apply --revert-check to inject temporary drift" 1
    skip_stage "revert-check-drift-diff" "pass --apply --revert-check to inspect temporary drift"
    skip_stage "revert-check-revert" "pass --apply --revert-check to restore temporary drift" 1
    skip_stage "revert-check-clean-diff" "pass --apply --revert-check to verify reverted state"
    skip_stage "validate-cluster-after-revert" "pass --apply --revert-check to validate after revert" 1
    return
  fi
  if [[ "${EUID}" -ne 0 ]]; then
    fail "--apply requires root because sync apply mutates host files and services"
  fi

  run_to_file "apply" --mutates "${SEALOS_BIN}" sync apply \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}" \
    --host-root "${HOST_ROOT}"
  run_to_file "status" "${SEALOS_BIN}" sync status \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}" \
    --host-root "${HOST_ROOT}"
  run_to_file "diff" "${SEALOS_BIN}" sync diff \
    --runtime-root "${RUNTIME_ROOT}" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}" \
    --host-root "${HOST_ROOT}"
  run_to_file "validate-cluster" --mutates "${POC_DIR}/validate.sh" \
    --cluster "${CLUSTER_NAME}" \
    --bundle-dir "${BUNDLE_DIR}" \
    --kubeconfig "${KUBECONFIG_PATH}"
  revert_check
}

main() {
  RUN_STARTED_AT="$(now_utc)"
  trap write_acceptance_report_on_exit EXIT

  parse_args "$@"
  build_sealos
  resolve_sealos_bin
  check_sealos_support
  init_paths
  resolve_package_mode

  inspect_packages
  build_packages
  init_local_repo
  fill_local_repo_inputs
  doctor_local_repo
  validate_sources
  source_preflight
  render_bundle
  verify_bundle_source_preflight
  runtime_preflight
  plan_bundle
  apply_and_observe
  write_acceptance_report
  check_acceptance_report

  log "smoke completed"
  printf 'workdir: %s\n' "${WORKDIR}"
  printf 'runtimeRoot: %s\n' "${RUNTIME_ROOT}"
  printf 'localRepo: %s\n' "${LOCAL_REPO}"
  printf 'bundleDir: %s\n' "${BUNDLE_DIR}"
  printf 'acceptanceReport: %s\n' "$(report_path)"
}

main "$@"
