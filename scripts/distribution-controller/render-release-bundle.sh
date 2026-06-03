#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

IMAGE="${DISTRIBUTION_CONTROLLER_IMAGE:-}"
OUTPUT_DIR="${DISTRIBUTION_CONTROLLER_BUNDLE_DIR:-${REPO_ROOT}/dist/distribution-controller}"
BASE_DIR="${REPO_ROOT}/deploy/distribution-controller/base"
EXAMPLES_DIR="${REPO_ROOT}/deploy/distribution-controller/examples"
BUILD_IMAGE=0
PUSH_IMAGE=0
PLATFORM="${PLATFORM:-linux_$(go env GOARCH)}"

usage() {
  cat <<'EOF'
Usage:
  render-release-bundle.sh --image IMAGE [options]

Renders a distribution controller install bundle under dist/ by default.

Options:
  --image IMAGE       Controller image to write into install.yaml.
  --output-dir DIR    Bundle output directory. Default: dist/distribution-controller.
  --platform NAME     Build platform used when --build-image is set. Default: linux_$(go env GOARCH).
  --build-image       Build the sealos-agent binary and local controller image before rendering.
  --push-image        Push the built controller image. Requires --build-image.
  -h, --help          Show this help.
EOF
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

log() {
  printf '==> %s\n' "$*"
}

shell_quote() {
  printf "'%s'" "${1//\'/\'\"\'\"\'}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)
      IMAGE="${2:?missing value for --image}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:?missing value for --output-dir}"
      shift 2
      ;;
    --platform)
      PLATFORM="${2:?missing value for --platform}"
      shift 2
      ;;
    --build-image)
      BUILD_IMAGE=1
      shift
      ;;
    --push-image)
      PUSH_IMAGE=1
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
[[ -d "${BASE_DIR}" ]] || fail "base manifest directory not found: ${BASE_DIR}"
command -v kubectl >/dev/null 2>&1 || fail "required command not found: kubectl"

IMAGE_BASENAME="${IMAGE##*/}"
if [[ "${IMAGE_BASENAME}" != *:* || "${IMAGE}" == *@* ]]; then
  fail "--image must be a tag reference such as ghcr.io/labring/sealos-agent:v1.2.3"
fi
IMAGE_NAME="${IMAGE%:*}"
IMAGE_TAG="${IMAGE##*:}"
[[ -n "${IMAGE_NAME}" && -n "${IMAGE_TAG}" ]] || fail "invalid --image ${IMAGE}"

if (( PUSH_IMAGE != 0 && BUILD_IMAGE == 0 )); then
  fail "--push-image requires --build-image"
fi

if (( BUILD_IMAGE != 0 )); then
  command -v docker >/dev/null 2>&1 || fail "required command not found: docker"
  command -v make >/dev/null 2>&1 || fail "required command not found: make"
  log "build sealos-agent for ${PLATFORM}"
  (cd "${REPO_ROOT}" && make build BINS=sealos-agent PLATFORM="${PLATFORM}")
  cp "${REPO_ROOT}/bin/${PLATFORM}/sealos-agent" "${REPO_ROOT}/docker/sealos-agent/sealos-agent"
  log "build controller image ${IMAGE}"
  docker build -t "${IMAGE}" "${REPO_ROOT}/docker/sealos-agent"
  if (( PUSH_IMAGE != 0 )); then
    log "push controller image ${IMAGE}"
    docker push "${IMAGE}"
  fi
fi

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/sealos-controller-bundle.XXXXXX")"
trap 'rm -rf "${TMP_DIR}"' EXIT

mkdir -p "${TMP_DIR}/overlay"
cp "${BASE_DIR}/namespace.yaml" "${TMP_DIR}/overlay/namespace.yaml"
cp "${BASE_DIR}/crd.yaml" "${TMP_DIR}/overlay/crd.yaml"
cp "${BASE_DIR}/rbac.yaml" "${TMP_DIR}/overlay/rbac.yaml"
cp "${BASE_DIR}/deployment.yaml" "${TMP_DIR}/overlay/deployment.yaml"
cat > "${TMP_DIR}/overlay/kustomization.yaml" <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - namespace.yaml
  - crd.yaml
  - rbac.yaml
  - deployment.yaml
images:
  - name: labring/sealos-agent:dev
    newName: ${IMAGE_NAME}
    newTag: ${IMAGE_TAG}
EOF

rm -rf "${OUTPUT_DIR}"
mkdir -p "${OUTPUT_DIR}/examples"

log "render install bundle"
kubectl kustomize "${TMP_DIR}/overlay" > "${OUTPUT_DIR}/install.yaml"
cp "${BASE_DIR}/namespace.yaml" "${OUTPUT_DIR}/namespace.yaml"
cp "${BASE_DIR}/crd.yaml" "${OUTPUT_DIR}/crd.yaml"
cp "${BASE_DIR}/rbac.yaml" "${OUTPUT_DIR}/rbac.yaml"
cp "${BASE_DIR}/deployment.yaml" "${OUTPUT_DIR}/deployment.template.yaml"
cp "${EXAMPLES_DIR}"/*.yaml "${OUTPUT_DIR}/examples/"

cat > "${OUTPUT_DIR}/README.md" <<EOF
# Sealos Distribution Controller Bundle

This bundle was rendered from \`deploy/distribution-controller/base\`.

- Controller image: \`${IMAGE}\`
- Install manifest: \`install.yaml\`
- CRDs: \`crd.yaml\`
- RBAC: \`rbac.yaml\`
- Deployment template: \`deployment.template.yaml\`
- Examples: \`examples/\`

Install:

\`\`\`bash
kubectl apply -f install.yaml
kubectl -n sealos-system rollout status deploy/sealos-distribution-controller --timeout=120s
\`\`\`

The controller still expects BOM or ReleaseChannel files and any local repo
inputs to be staged under paths visible from the controller pod.
EOF

log "bundle written to ${OUTPUT_DIR}"
