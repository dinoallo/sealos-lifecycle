#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

GO_TAGS="${GO_TAGS:-containers_image_openpgp exclude_graphdriver_devicemapper exclude_graphdriver_btrfs}"
GO_TEST_COUNT="${GO_TEST_COUNT:-1}"

usage() {
  cat <<'EOF'
Usage:
  acceptance.sh [options]

Runs the safe multi-node Day 0 acceptance gate. The script does not mutate the
host or contact remote nodes. It verifies the CLI render/plan contract for the
PoC package set against a three-node inventory, then runs reconcile tests that
exercise multi-node target execution, kubeadm join config generation, and
remote first-master kubeconfig fetches with fake remotes.

Options:
  --go-tags TAGS
  --count N
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
      --go-tags)
        GO_TAGS="${2:?missing value for --go-tags}"
        shift 2
        ;;
      --count)
        GO_TEST_COUNT="${2:?missing value for --count}"
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
}

run_go_test() {
  local package="$1"
  local pattern="$2"
  shift 2

  log "go test ${package} -run ${pattern}"
  go test -tags "${GO_TAGS}" "${package}" -run "${pattern}" -count="${GO_TEST_COUNT}" "$@"
}

main() {
  parse_args "$@"
  cd "${REPO_ROOT}"

  run_go_test ./cmd/sealos/cmd \
    'TestSyncDay0MultiNodePOCAcceptance|TestSyncPlanCmdSummarizesTargetsAndRedactsSecretContent|TestSyncApplyCmdDelegatesMultiNodeBundleToApplyEngine'

  run_go_test ./pkg/distribution/reconcile \
    'TestApplyRespectsExecutionTargetsOnMultiNodeCluster|TestApplyGeneratesPerHostKubeadmJoinConfigsForMultiNodeBootstrap|TestApplyFetchesKubeconfigFromRemoteFirstMasterForClusterSteps|TestRenderKubeadmJoinConfigResolvesHostnameAdvertiseAddress|TestRunKubeadmBootstrapHookRepairsJoinedSecondaryControlPlaneWithoutFreshHosts|TestRunKubeadmBootstrapHookRepairsJoinedFirstControlPlaneWithoutFreshHosts'

  log "multi-node Day 0 acceptance passed"
}

main "$@"
