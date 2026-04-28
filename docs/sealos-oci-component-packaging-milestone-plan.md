# Plan: OCI Component Packaging Milestone

## Status

Completed on this machine

## Source

This plan is derived from:

- `docs/sealos-component-package-format-design.md`
- the current `sync render` and `sync apply` implementation
- the verified minimal single-node package PoC in this repo

## Goal

Make OCI-packaged components the canonical input for `sealos sync render` on a
prepared single-node host.

Concretely, the milestone should prove this flow:

1. package a component directory as an OCI image that contains `package.yaml`
2. push that image to a registry
3. reference the pushed image and digest from the BOM
4. run `sealos sync render` without local `--package-source` overrides
5. produce the same rendered bundle shape that `sync apply` already consumes

This milestone is about OCI-backed render input. It is not a multi-node
milestone, not an upgrade milestone, and not a promotion/signing milestone.

## Current Repo Reality

The repo is closer to this milestone than the CLI surface currently suggests.

What already exists:

- BOM component artifacts already carry `image` and `digest` fields in
  `pkg/distribution/bom/types.go`
- the package format already models components as OCI-backed package payloads in
  `pkg/distribution/packageformat`
- `packageformat.MountedImageLoader` already loads `package.yaml` from a mounted
  image
- `hydrate.NewMountedArtifactSourceProvider` already renders package content
  from mounted artifacts
- `processor.NewPackageImageMounter` already pulls, creates, and mounts images
  through the repo's existing buildah path
- `cmd/sealos/cmd/sync.go` already falls back to the mounted-image path when a
  component is not supplied by `--package-source`
- `cmd/sealos/cmd/sync_test.go` now covers render from BOM-only OCI artifact
  references without real registry access
- `pkg/distribution/hydrate/render_test.go` now covers render through the
  mounted-artifact source provider
- `scripts/distribution/oci-package/build.sh` now builds deterministic package
  images from package directories and disables manifest image saving during
  package-image creation
- `scripts/distribution/oci-package/push.sh` now pushes those images and prints
  BOM-ready `image` and `digest` output
- `scripts/poc/minimal-single-node/publish-oci.sh` now stages a temporary
  package root, builds and pushes all three PoC package images, and writes an
  OCI-backed BOM
- `scripts/poc/minimal-single-node/render.sh` now supports `--package-mode oci`
  for BOM-only render from pushed OCI package images

What this milestone now proves:

- package directories can be turned into OCI images with `package.yaml` at image
  root
- pushed `image` and `digest` references can be written back into a BOM
- `sealos sync render` works with no `--package-source` overrides for the full
  three-package minimal single-node PoC
- the existing single-node `sync apply` path still succeeds on the rendered OCI
  bundle

## Milestone Boundary

### In Scope

- OCI image packaging for a component directory that already follows the
  `package.yaml` layout
- registry push and pull for those package images
- `sealos sync render` from BOM artifact references with no local package source
- reuse of the current prepared single-node `sync apply` path with no behavior
  expansion
- focused unit and integration coverage for the OCI render path

### Out Of Scope

- generic multi-node apply behavior
- upgrade, rollback, or drift workflows
- signing, provenance, or signature verification
- release-channel promotion flows
- a full cache manager beyond the current pull-if-missing behavior
- broad registry auth UX beyond existing containers/buildah auth behavior
- replacing OCI images with a new non-image artifact transport in this
  milestone

## Key Design Decisions For This Milestone

### 1. Use OCI Images, Not A New Artifact Runtime

The current code mounts packages through buildah and expects a mountable image
filesystem. That means the milestone should package components as OCI images
containing the package tree at the image root.

Do not widen this milestone into custom OCI artifact media types or non-image
mount semantics.

### 2. Keep `sync apply` Unchanged

`sync apply` already consumes a rendered filesystem bundle. The OCI milestone
should stop at render input. Once the bundle is rendered, apply should behave
exactly as it does today.

### 3. Keep Packaging UX Narrow

The shortest path is not a full productized `sealos sync package` command yet.
For this milestone, a repo helper script is enough if it is deterministic and
testable.

Recommended initial packaging surface:

- `scripts/distribution/oci-package/build.sh`
- `scripts/distribution/oci-package/push.sh`

If the helper path proves stable, a later milestone can promote it into CLI
subcommands.

### 4. Keep Local `--package-source` As A Dev Override

Do not remove `--package-source`. It is still useful for local iteration and
tests. The contract should change from "required PoC path" to "developer
override path."

## Acceptance Criteria

This milestone is complete when all of the following are true:

1. A component package directory can be turned into an OCI image with
   `package.yaml` at the image root.
2. The image can be pushed to a registry and referenced from a BOM by image and
   digest.
3. `sealos sync render` succeeds without `--package-source` for the minimal
   single-node package set.
4. The rendered bundle is accepted by the existing single-node `sync apply`
   workflow without OCI-specific apply changes.
5. Targeted tests cover the BOM-only OCI render path.
6. Docs describe OCI as the default package transport for this workflow and
   local package dirs as an override.

## Repo Fit

### Existing Paths To Reuse

| Path | Reuse In This Milestone |
| --- | --- |
| `cmd/sealos/cmd/sync.go` | Keep `sync render` as the user entrypoint and tighten its wording around OCI package sources. |
| `pkg/distribution/packageformat/load.go` | Reuse `MountedImageLoader` for package manifest loading from OCI images. |
| `pkg/distribution/hydrate/render.go` | Reuse `MountedArtifactSourceProvider` for package payload rendering from mounted images. |
| `pkg/apply/processor/package_image_mounter.go` | Reuse the buildah-backed pull/create/mount path for OCI package images. |
| `pkg/distribution/reconcile/materialize.go` | Keep render/materialize behavior unchanged once sources resolve. |
| `scripts/poc/minimal-single-node/` | Reuse the three-package PoC set as the first OCI-backed validation target. |

### Helper Paths Added In This Phase

| Path | Responsibility |
| --- | --- |
| `scripts/distribution/oci-package/build.sh` | Build a package directory into an OCI image deterministically. |
| `scripts/distribution/oci-package/push.sh` | Push the built image and print the resolved digest/reference data needed by the BOM. |
| `scripts/distribution/oci-package/common.sh` | Shared helper logic for package metadata, sealos resolution, and destination normalization. |
| `scripts/distribution/oci-package/metadata/` | Repo-local package metadata and path inspection used by the helper scripts. |
| `cmd/sealos/cmd/sync_test.go` | Add command-level coverage for render from OCI-backed package sources. |
| `pkg/distribution/hydrate/render_test.go` | Add lower-level render coverage through the mounted-artifact source provider. |
| `scripts/poc/minimal-single-node/publish-oci.sh` | Publish the full three-package PoC set to OCI and write a render-ready BOM. |
| `docs/sealos-minimal-k8s-package-poc-plan.md` | Update the PoC doc once OCI render is proven so local package dirs are no longer the only documented path. |

## Recommended Implementation Order

### Phase 0: Lock The Package Image Contract

Goal: define exactly how a package directory becomes a mountable OCI image.

Tasks:

- require `package.yaml` at image root
- preserve the existing package directory layout inside the image root
- choose a deterministic image build recipe for package directories
- decide which OCI labels are required versus optional for this milestone

Exit criteria:

- one package directory can be built into a mountable OCI image and loaded back
  through `packageformat.LoadFromImage`

### Phase 1: Add Repo Helper Packaging Scripts

Goal: create the narrowest repo-owned path for building and pushing package
images.

Tasks:

- add a helper that validates a package directory before image build
- add a helper that builds an OCI image from that directory
- add a helper that pushes the image and prints the resulting digest
- use the helper to package the three minimal single-node PoC components

Exit criteria:

- the repo can produce registry-pushable OCI package images for
  `containerd`, `kubernetes`, and `cilium`

### Phase 2: Make OCI The Primary `sync render` Path

Goal: make BOM-only OCI references the main supported render path.

Tasks:

- tighten `sync render` help text so `--package-source` is clearly an override
- keep the current fallback loader/source path, but test it as the primary
  milestone behavior
- improve render errors for missing image mounts, missing `package.yaml`, and
  missing package content inside the mounted image

Exit criteria:

- `sealos sync render` succeeds for the OCI-packaged minimal PoC BOM with no
  local package overrides

### Phase 3: Verification And Regression Coverage

Goal: prove the OCI-backed render flow is real and does not regress apply.

Tasks:

- add command-level tests that exercise the OCI-backed render path
- add lower-level loader/render tests where failure surfaces are easier to
  isolate
- validate the real host flow:
  - build and push package images
  - update BOM references
  - run `sealos sync render`
  - run `sealos sync apply`

Exit criteria:

- the same prepared single-node host can render from OCI and still apply
  successfully

Status:

- completed on this machine

## Verification Strategy

### Unit And Package-Level

- `go test ./pkg/distribution/packageformat ./pkg/distribution/hydrate ./pkg/distribution/bom`
- targeted `go test ./cmd/sealos/cmd -run 'TestSync.*'`

Focus:

- package image load success and failure behavior
- render from mounted artifact sources
- CLI behavior when OCI package resolution succeeds or fails

### Host-Level

Use the existing minimal single-node PoC as the first real validation target.

Recommended host verification:

1. package and push the three PoC packages to a reachable registry
2. point the PoC BOM at those pushed image references and digests
3. run `sealos sync render` with no `--package-source`
4. run `sealos sync apply`
5. validate node readiness and system pod health

## Risks And Guardrails

### Risk: Scope Creep Into Packaging UX

Guardrail:

- keep build/push as helper scripts first
- defer first-class packaging CLI until OCI render input is proven

### Risk: Scope Creep Into Registry Auth And Caching

Guardrail:

- reuse existing buildah/container auth behavior
- keep cache behavior at pull-if-missing for this milestone

### Risk: Confusing OCI Images With A Different Artifact Runtime

Guardrail:

- explicitly state that this milestone uses mountable OCI images because the
  repo's render path already depends on that behavior

### Risk: Testing Becomes Registry-Heavy Too Early

Guardrail:

- keep most coverage at package and command level
- use one prepared-host validation path as the real integration proof

## Deferred Follow-Ups

These should not block the milestone:

- productized `sealos sync package` or `sealos sync push` commands
- digest/signature verification policy
- mirror and offline cache management
- promotion workflows and release channels
- non-image OCI artifact media types
- multi-node OCI-backed apply

## Recommended Definition Of Done

Call the milestone done when:

- the minimal package PoC can render from OCI package images with no local
  package overrides
- `sync apply` still succeeds on the rendered bundle
- the repo contains targeted tests for the OCI render path
- the docs describe OCI package images as the canonical transport for this
  workflow
