# Copyright © 2022 sealos.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# ==============================================================================
# Build options

ROOT_PACKAGE=github.com/labring/sealos
VERSION_PACKAGE=github.com/labring/sealos/pkg/version

# ==============================================================================
# Includes

include scripts/make-rules/common.mk # must be the first to include
include scripts/make-rules/golang.mk
include scripts/make-rules/gen.mk
include scripts/make-rules/license.mk
include scripts/make-rules/tools.mk

# ==============================================================================
# Usage

define USAGE_OPTIONS

Options:
  DEBUG            Whether or not to generate debug symbols. Default is 0.

  ENABLE_BTRFS     Set to 1 to include the btrfs storage driver in Go builds.
                   By default btrfs is excluded so local builds/tests do not
                   require btrfs kernel headers.

  CC               Override the C compiler for CGO-enabled binaries.
                   Example: make build BINS=sealos CC=gcc

  CC_amd64         Override the C compiler for linux/amd64 CGO builds.
                   Example: make build.multiarch BINS=sealos CC_amd64=x86_64-linux-gnu-gcc

  CC_arm64         Override the C compiler for linux/arm64 CGO builds.
                   Example: make build.multiarch BINS=sealos CC_arm64=aarch64-linux-gnu-gcc

  BINS             Binaries to build. Default is all binaries under cmd.
                   This option is available when using: make {build}(.multiarch)
                   Example: make build BINS="sealos sealctl"

  SYNC_PACKAGE_SMOKE_ARGS
                   Extra arguments passed to scripts/poc/minimal-single-node/smoke.sh.
                   Example: make verify-sync-package-smoke SYNC_PACKAGE_SMOKE_ARGS="--skip-build"
                   The smoke script writes acceptance-report.yaml under its workdir
                   unless --report-file is passed here.

  I_UNDERSTAND_THIS_MUTATES_HOST
                   Must be set to 1 for verify-sync-package-apply and
                   verify-sync-package-revert.

  DISTRIBUTION_CONTROLLER_IMAGE
                   Controller image used by render-distribution-controller-bundle
                   and verify-distribution-controller-real-cluster.
                   Example: ghcr.io/labring/sealos-agent:v0.0.0

  DISTRIBUTION_CONTROLLER_BUNDLE_DIR
                   Output directory for render-distribution-controller-bundle.
                   Default is dist/distribution-controller.

  DISTRIBUTION_CONTROLLER_PUSH_IMAGE
                   Set to 1 with build-distribution-controller-image to push the
                   built image after docker build.

  DISTRIBUTION_CONTROLLER_SMOKE_ARGS
                   Extra arguments passed to scripts/distribution-controller/real-cluster-smoke.sh.
                   Example: "--kubeconfig ~/.kube/config --artifact-dir /tmp/controller-smoke --keep-resources"

  PLATFORMS        Platform to build for. Default is linux_arm64 and linux_amd64.
                   This option is available when using: make {build}.multiarch
                   Example: make build.multiarch PLATFORMS="linux_arm64 linux_amd64"

  V                Set to 1 enable verbose build. Default is 0.
endef
export USAGE_OPTIONS

# ==============================================================================
# Targets

.DEFAULT_GOAL = build

## build: Build source code for host platform.
.PHONY: build
build:
	@$(MAKE) go.build

## build.multiarch: Build source code for multiple platforms. See option PLATFORMS.
.PHONY: build.multiarch
build.multiarch:
	@$(MAKE) go.build.multiarch

# ## image: Build docker images for host platform.
# .PHONY: image
# image:
# 	@$(MAKE) image.build

# ## image.multiarch: Build docker images for multiple platforms. See option PLATFORMS.
# .PHONY: image.multiarch
# image.multiarch:
# 	@$(MAKE) image.build.multiarch

# ## push: Push docker images for host platform to registry.
# .PHONY: push
# push:
# 	@$(MAKE) image.push

# ## push.multiarch: Push docker images for multiple platforms to registry. See option PLATFORMS.
# .PHONY: push.multiarch
# push.multiarch:
# 	@$(MAKE) image.push.multiarch

## lint: Check syntax and styling of go sources.
.PHONY: lint
lint:
	@$(MAKE) go.lint

## format: Gofmt (reformat) package sources.
.PHONY: format
format:
	@$(MAKE) go.format

## coverage: Run unit tests and output test coverage.
.PHONY: coverage
coverage:
	@$(MAKE) go.coverage

## verify-sync-package-smoke: Run the safe minimal single-node package lifecycle smoke flow.
.PHONY: verify-sync-package-smoke
verify-sync-package-smoke:
	@scripts/poc/minimal-single-node/smoke.sh $(SYNC_PACKAGE_SMOKE_ARGS)

## verify-sync-package-apply: Run the mutating minimal single-node package apply acceptance flow.
.PHONY: verify-sync-package-apply
verify-sync-package-apply:
	@set -eu; \
	if [ "$(I_UNDERSTAND_THIS_MUTATES_HOST)" != "1" ]; then \
		echo "I_UNDERSTAND_THIS_MUTATES_HOST=1 is required because this target mutates the host" >&2; \
		exit 1; \
	fi; \
	scripts/poc/minimal-single-node/smoke.sh --apply $(SYNC_PACKAGE_SMOKE_ARGS)

## verify-sync-package-revert: Run mutating apply plus scoped drift/revert acceptance.
.PHONY: verify-sync-package-revert
verify-sync-package-revert:
	@set -eu; \
	if [ "$(I_UNDERSTAND_THIS_MUTATES_HOST)" != "1" ]; then \
		echo "I_UNDERSTAND_THIS_MUTATES_HOST=1 is required because this target mutates the host" >&2; \
		exit 1; \
	fi; \
	scripts/poc/minimal-single-node/smoke.sh --apply --revert-check $(SYNC_PACKAGE_SMOKE_ARGS)

## build-distribution-controller-image: Build the sealos-agent controller image for testing or release preparation.
.PHONY: build-distribution-controller-image
build-distribution-controller-image:
	@set -eu; \
	if [ -z "$(DISTRIBUTION_CONTROLLER_IMAGE)" ]; then \
		echo "DISTRIBUTION_CONTROLLER_IMAGE is required" >&2; \
		exit 1; \
	fi; \
	set -- --image "$(DISTRIBUTION_CONTROLLER_IMAGE)" --build-image; \
	if [ "$(DISTRIBUTION_CONTROLLER_PUSH_IMAGE)" = "1" ]; then set -- "$$@" --push-image; fi; \
	scripts/distribution-controller/render-release-bundle.sh "$$@"

## render-distribution-controller-bundle: Render release-ready controller install manifests.
.PHONY: render-distribution-controller-bundle
render-distribution-controller-bundle:
	@set -eu; \
	if [ -z "$(DISTRIBUTION_CONTROLLER_IMAGE)" ]; then \
		echo "DISTRIBUTION_CONTROLLER_IMAGE is required" >&2; \
		exit 1; \
	fi; \
	scripts/distribution-controller/render-release-bundle.sh \
		--image "$(DISTRIBUTION_CONTROLLER_IMAGE)" \
		$(if $(DISTRIBUTION_CONTROLLER_BUNDLE_DIR),--output-dir "$(DISTRIBUTION_CONTROLLER_BUNDLE_DIR)")

## verify-distribution-controller-manifests: Validate controller manifests and controller wiring.
.PHONY: verify-distribution-controller-manifests
verify-distribution-controller-manifests:
	TMPDIR=/tmp GOCACHE=$(TMP_DIR)/go-build go test -tags "$(GO_TAGS)" ./deploy/distribution-controller ./pkg/distribution/controller ./cmd/sealos-agent/cmd -count=1
	kubectl kustomize deploy/distribution-controller/base >/dev/null

## verify-distribution-controller-real-cluster: Run the mutating controller install smoke against a real cluster.
.PHONY: verify-distribution-controller-real-cluster
verify-distribution-controller-real-cluster:
	@set -eu; \
	if [ "$(I_UNDERSTAND_THIS_MUTATES_HOST)" != "1" ]; then \
		echo "I_UNDERSTAND_THIS_MUTATES_HOST=1 is required because this target mutates a Kubernetes cluster" >&2; \
		exit 1; \
	fi; \
	if [ -z "$(DISTRIBUTION_CONTROLLER_IMAGE)" ]; then \
		echo "DISTRIBUTION_CONTROLLER_IMAGE is required" >&2; \
		exit 1; \
	fi; \
	scripts/distribution-controller/real-cluster-smoke.sh \
		--image "$(DISTRIBUTION_CONTROLLER_IMAGE)" \
		--apply \
		$(DISTRIBUTION_CONTROLLER_SMOKE_ARGS)

## verify-local-patch-policy: Validate LocalPatchPolicy parsing, provenance, and materialization guards.
.PHONY: verify-local-patch-policy
verify-local-patch-policy:
	go test -tags "$(GO_TAGS)" ./pkg/distribution/ownership ./pkg/distribution/hydrate ./pkg/distribution/localrepo ./pkg/distribution/policyreport ./pkg/distribution/reconcile ./pkg/distribution/compare ./pkg/distribution/commit -count=1

## verify-local-patch-policy-gate: Evaluate a LocalPatchPolicy change with the same gate semantics used in CI.
.PHONY: verify-local-patch-policy-gate
verify-local-patch-policy-gate:
	@set -eu; \
	if [ -z "$(OLD_POLICY)" ]; then echo "OLD_POLICY is required" >&2; exit 1; fi; \
	if [ -z "$(NEW_POLICY)" ]; then echo "NEW_POLICY is required" >&2; exit 1; fi; \
	set -- sync policy-gate --old-policy "$(OLD_POLICY)" --new-policy "$(NEW_POLICY)"; \
	if [ -n "$(LOCAL_REPO)" ]; then set -- "$$@" --local-repo "$(LOCAL_REPO)"; fi; \
	if [ -n "$(APPROVAL_FILE)" ]; then set -- "$$@" --approval-file "$(APPROVAL_FILE)"; fi; \
	if [ -n "$(APPROVAL_EXPIRY_WARNING_DAYS)" ]; then set -- "$$@" --approval-expiry-warning-days "$(APPROVAL_EXPIRY_WARNING_DAYS)"; fi; \
	if [ -n "$(FAIL_WHEN_APPROVAL_EXPIRES_SOON)" ]; then set -- "$$@" --fail-when-approval-expires-soon; fi; \
	go run -tags "$(GO_TAGS)" ./cmd/sealos "$$@"

## verify-local-patch-policy-approvals: Scan LocalPatchPolicyGateApproval files for invalid, expired, or near-expiry exceptions.
.PHONY: verify-local-patch-policy-approvals
verify-local-patch-policy-approvals:
	@set -eu; \
	root='$(if $(APPROVAL_SCAN_ROOT),$(APPROVAL_SCAN_ROOT),.)'; \
	set -- sync policy-approval-scan --root "$$root"; \
	if [ -n "$(APPROVAL_EXPIRY_WARNING_DAYS)" ]; then set -- "$$@" --approval-expiry-warning-days "$(APPROVAL_EXPIRY_WARNING_DAYS)"; fi; \
	if [ -n "$(FAIL_WHEN_APPROVAL_EXPIRES_SOON)" ]; then set -- "$$@" --fail-when-approval-expires-soon; fi; \
	go run -tags "$(GO_TAGS)" ./cmd/sealos "$$@"

## verify-license: Verify the license headers for all files.
.PHONY: verify-license
verify-license:
	@$(MAKE) license.verify

## add-license: Ensure source code files have license headers.
.PHONY: add-license
add-license:
	@$(MAKE) license.add

##license: Add license header to all files.
.PHONY: license
license:
	@$(MAKE) license.add

.PHONY: license.controller
license.controller:
	@$(MAKE) license.controller.add

## gen: Generate all necessary files.
.PHONY: gen
gen:
	@$(MAKE) gen.run

## tools: Install dependent tools.
.PHONY: tools
tools:
	@$(MAKE) tools.install

## clean: Remove all files that are created by building.
.PHONY: clean
clean:
	@echo "===========> Cleaning all build output"
	@-rm -vrf $(OUTPUT_DIR) $(BIN_DIR)

## help: Show this help info.
.PHONY: help
help: Makefile
	@echo -e "\nUsage: make <TARGETS> <OPTIONS> ...\n\nTargets:"
	@sed -n 's/^##//p' $< | awk -F':' '{printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' | sed -e 's/^/ /'
	@echo "$$USAGE_OPTIONS"
