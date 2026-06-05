#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

IMAGE="${DISTRIBUTION_CONTROLLER_IMAGE:-}"
PROFILE="${DISTRIBUTION_CONTROLLER_PROFILE:-host-agent}"
MANIFEST_DIR="${DISTRIBUTION_CONTROLLER_MANIFEST_DIR:-${REPO_ROOT}/deploy/distribution-controller}"
KUBECONFIG_PATH="${KUBECONFIG:-}"
APPLY=0
KEEP_RESOURCES=0
TIMEOUT="${DISTRIBUTION_CONTROLLER_TIMEOUT:-120s}"
ARTIFACT_DIR="${DISTRIBUTION_CONTROLLER_ARTIFACT_DIR:-}"
TMP_DIR=""
SMOKE_STARTED=0

usage() {
  cat <<'EOF'
Usage:
  real-cluster-smoke.sh --image IMAGE --apply [options]

Validates the distribution controller install path against a real Kubernetes
cluster. Without --apply, the script only renders manifests locally.

Options:
  --image IMAGE          Controller image to install.
  --profile NAME         Install profile to render. Supported values:
                         host-agent, production-host-agent. Default: host-agent.
  --manifest-dir DIR     Manifest directory. Default: deploy/distribution-controller.
  --kubeconfig PATH      Kubeconfig to use. Defaults to KUBECONFIG/current context.
  --timeout DURATION     Rollout timeout. Default: 120s.
  --artifact-dir DIR     Directory for diagnostics collected on failure.
  --apply                Mutate the selected cluster.
  --keep-resources       Do not delete the sample DistributionTarget/Policy after apply.
  -h, --help             Show this help.
EOF
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

log() {
  printf '==> %s\n' "$*"
}

kubectl_cmd() {
  if [[ -n "${KUBECONFIG_PATH}" ]]; then
    kubectl --kubeconfig "${KUBECONFIG_PATH}" "$@"
  else
    kubectl "$@"
  fi
}

collect_diagnostics() {
  local exit_code="$1"

  if (( APPLY == 0 || SMOKE_STARTED == 0 || exit_code == 0 )); then
    return
  fi

  local out_dir="${ARTIFACT_DIR}"
  if [[ -z "${out_dir}" ]]; then
    out_dir="${TMP_DIR}/diagnostics"
  fi
  mkdir -p "${out_dir}"
  log "collect diagnostics in ${out_dir}"

  {
    printf 'exitCode: %s\n' "${exit_code}"
    printf 'image: %s\n' "${IMAGE}"
    printf 'profile: %s\n' "${PROFILE}"
    printf 'manifestDir: %s\n' "${MANIFEST_DIR}"
    printf 'timeout: %s\n' "${TIMEOUT}"
    printf 'keepResources: %s\n' "${KEEP_RESOURCES}"
    date -u '+collectedAt: %Y-%m-%dT%H:%M:%SZ'
  } > "${out_dir}/summary.yaml"

  kubectl_cmd version -o yaml > "${out_dir}/kubectl-version.yaml" 2>&1 || true
  kubectl_cmd cluster-info > "${out_dir}/cluster-info.txt" 2>&1 || true
  kubectl_cmd get crd distributiontargets.distribution.sealos.io distributionrolloutpolicies.distribution.sealos.io -o yaml > "${out_dir}/crds.yaml" 2>&1 || true
  kubectl_cmd -n sealos-system get deploy,rs,pod,events -o wide > "${out_dir}/sealos-system-resources.txt" 2>&1 || true
  kubectl_cmd -n sealos-system describe deploy/sealos-distribution-controller > "${out_dir}/controller-deployment.describe.txt" 2>&1 || true
  kubectl_cmd -n sealos-system get pods -l app.kubernetes.io/name=sealos-distribution-controller -o yaml > "${out_dir}/controller-pods.yaml" 2>&1 || true
  kubectl_cmd -n sealos-system describe pods -l app.kubernetes.io/name=sealos-distribution-controller > "${out_dir}/controller-pods.describe.txt" 2>&1 || true
  kubectl_cmd -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent --tail=300 > "${out_dir}/controller.log" 2>&1 || true
  kubectl_cmd -n sealos-system get distributionrolloutpolicy controller-smoke -o yaml > "${out_dir}/controller-smoke-policy.yaml" 2>&1 || true
  kubectl_cmd -n sealos-system get distributiontarget controller-smoke -o yaml > "${out_dir}/controller-smoke-target.yaml" 2>&1 || true
  kubectl_cmd -n sealos-system describe distributiontarget controller-smoke > "${out_dir}/controller-smoke-target.describe.txt" 2>&1 || true
}

cleanup() {
  local exit_code="$?"
  collect_diagnostics "${exit_code}"
  if [[ -n "${TMP_DIR}" ]]; then
    rm -rf "${TMP_DIR}"
  fi
}
trap cleanup EXIT

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)
      IMAGE="${2:?missing value for --image}"
      shift 2
      ;;
    --profile)
      PROFILE="${2:?missing value for --profile}"
      shift 2
      ;;
    --manifest-dir)
      MANIFEST_DIR="${2:?missing value for --manifest-dir}"
      shift 2
      ;;
    --kubeconfig)
      KUBECONFIG_PATH="${2:?missing value for --kubeconfig}"
      shift 2
      ;;
    --timeout)
      TIMEOUT="${2:?missing value for --timeout}"
      shift 2
      ;;
    --artifact-dir)
      ARTIFACT_DIR="${2:?missing value for --artifact-dir}"
      shift 2
      ;;
    --apply)
      APPLY=1
      shift
      ;;
    --keep-resources)
      KEEP_RESOURCES=1
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

[[ -n "${IMAGE}" ]] || fail "--image or DISTRIBUTION_CONTROLLER_IMAGE is required"
[[ -d "${MANIFEST_DIR}/base" ]] || fail "manifest base directory not found: ${MANIFEST_DIR}/base"
command -v kubectl >/dev/null 2>&1 || fail "required command not found: kubectl"

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/sealos-controller-smoke.XXXXXX")"
BUNDLE_DIR="${TMP_DIR}/bundle"
DEPLOYMENT="${BUNDLE_DIR}/deployment.template.yaml"
POLICY="${TMP_DIR}/distribution-rollout-policy.yaml"
TARGET="${TMP_DIR}/distribution-target.yaml"

log "render ${PROFILE} install manifests"
"${REPO_ROOT}/scripts/distribution-controller/render-release-bundle.sh" \
  --image "${IMAGE}" \
  --profile "${PROFILE}" \
  --manifest-dir "${MANIFEST_DIR}" \
  --output-dir "${BUNDLE_DIR}" >/dev/null

if (( APPLY == 0 )); then
  log "cluster validation skipped because --apply was not set"
  exit 0
fi

log "validate cluster access"
SMOKE_STARTED=1
kubectl_cmd version --client=true >/dev/null
kubectl_cmd cluster-info >/dev/null

log "install controller manifests"
kubectl_cmd apply -f "${BUNDLE_DIR}/namespace.yaml"
kubectl_cmd apply -f "${BUNDLE_DIR}/crd.yaml"
kubectl_cmd wait --for=condition=Established crd/distributiontargets.distribution.sealos.io --timeout=60s
kubectl_cmd wait --for=condition=Established crd/distributionrolloutpolicies.distribution.sealos.io --timeout=60s
kubectl_cmd apply -f "${BUNDLE_DIR}/rbac.yaml"
kubectl_cmd apply -f "${DEPLOYMENT}"
kubectl_cmd -n sealos-system rollout status deploy/sealos-distribution-controller --timeout="${TIMEOUT}"

cat > "${POLICY}" <<'EOF'
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionRolloutPolicy
metadata:
  name: controller-smoke
  namespace: sealos-system
spec:
  strategy:
    batchSize: 1
    failureAction: Stop
EOF

cat > "${TARGET}" <<'EOF'
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionTarget
metadata:
  name: controller-smoke
  namespace: sealos-system
spec:
  clusterName: controller-smoke
  bomPath: /var/lib/sealos/distribution/controller-smoke/missing-bom.yaml
  rolloutPolicyRef:
    name: controller-smoke
EOF

log "exercise CRD admission and controller reconcile"
kubectl_cmd apply -f "${POLICY}"
kubectl_cmd apply -f "${TARGET}"
kubectl_cmd -n sealos-system wait --for=condition=Degraded distributiontarget/controller-smoke --timeout="${TIMEOUT}" || {
  kubectl_cmd -n sealos-system describe distributiontarget controller-smoke || true
  kubectl_cmd -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent --tail=200 || true
  fail "controller-smoke target did not become Degraded"
}
kubectl_cmd -n sealos-system get distributiontarget controller-smoke -o yaml >/dev/null
kubectl_cmd -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent --tail=100 >/dev/null

if (( KEEP_RESOURCES == 0 )); then
  log "cleanup smoke resources"
  kubectl_cmd -n sealos-system delete distributiontarget controller-smoke --ignore-not-found
  kubectl_cmd -n sealos-system delete distributionrolloutpolicy controller-smoke --ignore-not-found
fi

log "distribution controller real-cluster smoke passed"
