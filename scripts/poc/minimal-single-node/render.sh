#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

exec sealos sync render \
  --file "${REPO_ROOT}/scripts/poc/minimal-single-node/bom.yaml" \
  --cluster poc-minimal \
  --package-source "containerd=${REPO_ROOT}/scripts/poc/minimal-single-node/packages/containerd" \
  --package-source "kubernetes=${REPO_ROOT}/scripts/poc/minimal-single-node/packages/kubernetes" \
  --package-source "cilium=${REPO_ROOT}/scripts/poc/minimal-single-node/packages/cilium"
