#!/usr/bin/env bash
set -euo pipefail

REPORT_FILE=""
MODE="safe"

usage() {
  cat <<'EOF'
Usage:
  check-report.sh --report-file PATH [--mode safe|apply|revert]

Validates the PackageAcceptanceReport emitted by smoke.sh.
EOF
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --report-file)
        REPORT_FILE="${2:?missing value for --report-file}"
        shift 2
        ;;
      --mode)
        MODE="${2:?missing value for --mode}"
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

  [[ -n "${REPORT_FILE}" ]] || fail "--report-file is required"
  [[ -f "${REPORT_FILE}" ]] || fail "report file not found: ${REPORT_FILE}"
  case "${MODE}" in
    safe|apply|revert)
      ;;
    *)
      fail "unsupported mode: ${MODE} (want safe, apply, or revert)"
      ;;
  esac
}

yaml_value() {
  local key="$1"
  awk -v key="${key}" '
    index($0, key ":") == 1 || index($0, "  " key ":") == 1 {
      line = $0
      sub("^[[:space:]]*[^:]*:[[:space:]]*", "", line)
      gsub(/^"/, "", line)
      gsub(/"$/, "", line)
      print line
      exit
    }
  ' "${REPORT_FILE}"
}

stage_status() {
  local stage="$1"
  awk -v stage="${stage}" '
    $0 ~ /^  - name: / {
      in_stage = 0
      line = $0
      sub(/^  - name:[[:space:]]*/, "", line)
      gsub(/^"/, "", line)
      gsub(/"$/, "", line)
      if (line == stage) {
        in_stage = 1
      }
      next
    }
    in_stage && $0 ~ /^    status: / {
      line = $0
      sub(/^    status:[[:space:]]*/, "", line)
      gsub(/^"/, "", line)
      gsub(/"$/, "", line)
      print line
      exit
    }
  ' "${REPORT_FILE}"
}

stage_mutates() {
  local stage="$1"
  awk -v stage="${stage}" '
    $0 ~ /^  - name: / {
      in_stage = 0
      line = $0
      sub(/^  - name:[[:space:]]*/, "", line)
      gsub(/^"/, "", line)
      gsub(/"$/, "", line)
      if (line == stage) {
        in_stage = 1
      }
      next
    }
    in_stage && $0 ~ /^    mutates: / {
      line = $0
      sub(/^    mutates:[[:space:]]*/, "", line)
      print line
      exit
    }
  ' "${REPORT_FILE}"
}

require_value() {
  local key="$1"
  local value
  value="$(yaml_value "${key}")"
  [[ -n "${value}" ]] || fail "report field ${key} is required"
}

require_equals() {
  local key="$1"
  local expected="$2"
  local value
  value="$(yaml_value "${key}")"
  [[ "${value}" == "${expected}" ]] ||
    fail "report field ${key} = ${value:-<missing>}, want ${expected}"
}

require_stage_status() {
  local stage="$1"
  local expected="$2"
  local value
  value="$(stage_status "${stage}")"
  [[ "${value}" == "${expected}" ]] ||
    fail "stage ${stage} status = ${value:-<missing>}, want ${expected}"
}

require_stage_mutates() {
  local stage="$1"
  local expected="$2"
  local value
  value="$(stage_mutates "${stage}")"
  [[ "${value}" == "${expected}" ]] ||
    fail "stage ${stage} mutates = ${value:-<missing>}, want ${expected}"
}

require_no_secret_payloads() {
  local forbidden=(
    'stringData:'
    'password:'
    'token:'
    'client-secret:'
    'clientSecret:'
    'secretKey:'
  )
  local pattern
  for pattern in "${forbidden[@]}"; do
    if grep -qi -- "${pattern}" "${REPORT_FILE}"; then
      fail "report contains forbidden secret-like payload marker: ${pattern}"
    fi
  done
}

require_common_contract() {
  grep -q '^apiVersion: distribution.sealos.io/v1alpha1$' "${REPORT_FILE}" ||
    fail "report apiVersion is not distribution.sealos.io/v1alpha1"
  grep -q '^kind: PackageAcceptanceReport$' "${REPORT_FILE}" ||
    fail "report kind is not PackageAcceptanceReport"

  require_equals "status" "Passed"
  require_equals "exitCode" "0"
  require_value "clusterName"
  require_value "startedAt"
  require_value "finishedAt"
  require_value "bomFile"
  require_value "bomName"
  require_value "bomRevision"
  require_value "bomDigest"
  require_value "workdir"
  require_value "runtimeRoot"
  require_value "localRepo"
  require_value "bundleDir"
  require_value "desiredStateDigest"
  require_value "localRepoRevision"
  require_value "sourcePreflightState"
  require_value "runtimePreflightState"
  require_no_secret_payloads

  local common_passed=(
    package-inspect-containerd
    package-inspect-kubernetes
    package-inspect-cilium
    local-repo-init
    fill-local-repo-inputs
    local-repo-doctor
    validate
    source-preflight
    render
    verify-sourcePreflight-metadata
    runtime-preflight
    plan
  )
  local stage
  for stage in "${common_passed[@]}"; do
    require_stage_status "${stage}" "Passed"
  done
}

require_safe_contract() {
  require_equals "mutatingApply" "false"
  require_equals "revertCheck" "false"
  require_stage_status "apply" "Skipped"
  require_stage_status "status" "Skipped"
  require_stage_status "diff" "Skipped"
  require_stage_status "validate-cluster" "Skipped"
  require_stage_status "revert-check-drift-inject" "Skipped"
  require_stage_status "revert-check-drift-diff" "Skipped"
  require_stage_status "revert-check-revert" "Skipped"
  require_stage_status "revert-check-clean-diff" "Skipped"
  require_stage_status "validate-cluster-after-revert" "Skipped"
  require_stage_mutates "apply" "true"
  require_stage_mutates "revert-check-drift-inject" "true"
  require_stage_mutates "revert-check-revert" "true"
}

require_apply_contract() {
  require_equals "mutatingApply" "true"
  require_equals "revertCheck" "false"
  require_value "postApplyState"
  require_stage_status "apply" "Passed"
  require_stage_status "status" "Passed"
  require_stage_status "diff" "Passed"
  require_stage_status "validate-cluster" "Passed"
  require_stage_status "revert-check-drift-inject" "Skipped"
  require_stage_status "revert-check-drift-diff" "Skipped"
  require_stage_status "revert-check-revert" "Skipped"
  require_stage_status "revert-check-clean-diff" "Skipped"
  require_stage_status "validate-cluster-after-revert" "Skipped"
  require_stage_mutates "apply" "true"
  require_stage_mutates "validate-cluster" "true"
}

require_revert_contract() {
  require_equals "mutatingApply" "true"
  require_equals "revertCheck" "true"
  require_value "postApplyState"
  require_equals "postRevertState" "Clean"
  require_stage_status "apply" "Passed"
  require_stage_status "status" "Passed"
  require_stage_status "diff" "Passed"
  require_stage_status "validate-cluster" "Passed"
  require_stage_status "revert-check-drift-inject" "Passed"
  require_stage_status "revert-check-drift-diff" "Passed"
  require_stage_status "revert-check-revert" "Passed"
  require_stage_status "revert-check-clean-diff" "Passed"
  require_stage_status "validate-cluster-after-revert" "Passed"
  require_stage_mutates "apply" "true"
  require_stage_mutates "revert-check-drift-inject" "true"
  require_stage_mutates "revert-check-revert" "true"
}

main() {
  parse_args "$@"
  require_common_contract
  case "${MODE}" in
    safe)
      require_safe_contract
      ;;
    apply)
      require_apply_contract
      ;;
    revert)
      require_revert_contract
      ;;
  esac

  printf 'acceptance report passed: %s (%s)\n' "${REPORT_FILE}" "${MODE}"
}

main "$@"
