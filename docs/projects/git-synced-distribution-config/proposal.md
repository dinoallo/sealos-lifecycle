# Proposal: Git-Synced Distribution Configuration

## Status

Draft

## Summary

This document proposes a Git repository layout for synchronizing Sealos distribution configuration, including package manifests, build classes, distribution profiles, BOM files, and release channels.

The recommended model is to use Git as the source of truth for distribution configuration, release intent, and the source facts needed to materialize packages. Git stores small, reviewable YAML documents such as `package.yaml`, build classes, profiles, BOMs, and channel pointers. OCI remains the preferred transport and cache for prebuilt immutable package artifacts, but it is not the only way to materialize a package.

The proposal intentionally defines repository conventions and resolution rules first. It does not require Sealos to introduce new API types before teams can adopt the layout; formal schemas such as `ReleaseChannel` and `ClusterTarget` can be added later without changing the path model.

## Problem Statement

The current package and BOM model defines what a package is and how a distribution pins package revisions. It still needs a practical repository layout for teams that want to synchronize those files through Git.

Without a clear layout:

- package manifests and release BOMs can become mixed with generated render output
- different package types or providers can collide on short names such as `kubernetes`, `cilium`, or `cert-manager`
- release promotion becomes hard to review because channel movement is not isolated
- profile defaults and promotion policy may accidentally mix with package source definitions
- operators have no predictable path for finding the files that affect one distribution revision
- automation has to rely on ad hoc path conventions

## Goals

- Make package configuration and distribution BOMs easy to review in pull requests.
- Give every package a stable, collision-resistant identity.
- Distinguish package types in the repository layout and BOMs.
- Support both source-first local builds and prebuilt artifact consumption without changing the repository layout.
- Keep built package payloads out of Git, whether they are stored in OCI, a local registry, an OCI layout, or an agent cache.
- Separate package definitions, build classes, distribution profiles, release BOMs, and channel pointers.
- Define a clear boundary with separate `cluster-config` repositories for cluster-local targets, inputs, and patches.
- Make the repository layout friendly to pull-based synchronization from private clusters.
- Avoid committing generated render output by default.

## Non-Goals

- Replacing OCI as a package transport.
- Defining a complete GitOps controller implementation.
- Storing secret values in a shared Git repository.
- Making Git the only possible local configuration backend.
- Redesigning the existing package or `BOM` schema beyond the identity and layout conventions needed here.
- Defining repository hosting, authentication, or branch protection requirements.

## Design Principles

- Keep Git reviewable: store source YAML and small patches, not built package payloads or rendered bundles.
- Keep materialization immutable: BOMs should pin source digests and, when prebuilt artifacts are available, artifact image and digest.
- Separate ownership: global distribution content belongs in `distribution-config`; cluster-local targets, inputs, and patches belong in separate `cluster-config` repositories.
- Make promotion explicit: moving a channel should be a small, auditable change.
- Keep render deterministic: a Git revision plus source and artifact digests should be enough to reproduce the desired state.

## Recommended Repository Model

Use a distribution configuration repository for platform-owned content:

```text
distribution-config/
  packages/
    infra/
      kubernetes/
        v1.30.3/
          package.yaml
          files/
          manifests/
          hooks/
    network/
      cilium/
        v1.15.8/
          package.yaml
          values/
    policy/
      pod-security/
        v1.0.0/
          package.yaml
  classes/
    rootfs-image/
      v1.yaml
  profiles/
    default-platform/
      prod-amd64/
        defaults.yaml
        feature.mask.yaml
        package.mask.yaml
        support-matrix.yaml
  releases/
    default-platform/
      rev-20240424-prod/
        bom.yaml
  channels/
    default-platform/
      alpha.yaml
      beta.yaml
      stable.yaml
  policy/
    validation/
  README.md
```

This repository should contain the source files needed to build, validate, select, and render a distribution. It should not contain generated render output, downloaded OCI artifact contents, or locally built package artifacts.

Cluster-local configuration is intentionally outside this repository model. Use a separate `cluster-config` repository for `ClusterTarget`, local inputs, patches, delivery policy, and secret references.

## Directory Responsibilities

| Path | Responsibility |
| --- | --- |
| `packages/<category>/<name>/<version>/` | Source configuration for one package revision. |
| `packages/<category>/<name>/<version>/package.yaml` | The package manifest that will be copied into the materialized package root. |
| `classes/<class>/<version>.yaml` | Reusable build or render class definitions used by packages. |
| `profiles/<distribution>/<profile>/` | Distribution-level defaults, feature masks, package masks, and support matrix rules. |
| `releases/<distribution>/<revision>/bom.yaml` | A release BOM that pins source facts, build contracts, and optional package artifacts by digest. |
| `channels/<distribution>/<channel>.yaml` | A small pointer from a channel name to an approved BOM revision. |
| `policy/` | Validation rules used by CI or promotion automation. |

## What Belongs In Git

Git should store files that are small, intentional, and useful for review:

- package manifests
- package source files referenced by `package.yaml`
- build and render class definitions
- distribution profile defaults, masks, and support matrix rules
- release BOM files
- release channel pointer files
- validation policy and CI configuration
- documentation for package ownership and release process

Git should not store:

- built OCI image or local package artifact contents
- rendered desired-state bundles
- downloaded package artifacts
- node-local cache directories
- cluster target files, cluster-local inputs, and cluster-local patches
- private keys, tokens, certificates, or secret values
- large binary dependencies unless there is no practical artifact-store alternative

## Package Identity And Categories

Every package revision should have a canonical identity:

```text
<category>/<name>@<version>
```

The identity fields have separate responsibilities:

| Field | Purpose |
| --- | --- |
| `category` | Describes the role of the package in the distribution and provides the first level of name isolation. |
| `name` | The short package name within the category. |
| `version` | The package revision selected by a BOM. |

`category` and `name` should use lowercase DNS-label style segments. `version` should be a stable package revision string, usually an upstream semantic version or a distribution-owned revision.

Recommended initial categories:

| Category | Examples | Responsibility |
| --- | --- | --- |
| `infra` | `kubernetes`, `etcd` | Core cluster infrastructure required to form or maintain the cluster. |
| `runtime` | `containerd`, `cri-o` | Node or workload runtime packages. |
| `network` | `cilium`, `calico` | CNI, networking, and traffic infrastructure. |
| `addon` | `cert-manager`, `metrics-server` | Optional or replaceable cluster services. |
| `policy` | `pod-security`, `baseline` | Policy, admission, and compliance packages. |
| `tooling` | `sealctl`, `netshoot` | Operational tools that support lifecycle workflows. |
| `patch` | `kubeadm-hardening` | Reusable platform-owned overlays that are not cluster-local patches. |

The tuple `category/name` must be unique within a repository. Two packages may share the same `name` only when their `category` differs. BOMs, profiles, masks, and validation rules should refer to packages by full identity, not by short name alone.

Provider, owner, ecosystem, and upstream project names should be recorded as package metadata when useful, but they should not be part of the default package identity.

## Package Source Layout

Each package revision should be self-contained under `packages/<category>/<name>/<version>/`.

Example:

```text
packages/infra/kubernetes/v1.30.3/
  package.yaml
  files/
    etc/kubernetes/kubeadm.yaml
    etc/kubernetes/audit-policy.yaml
  manifests/
    bootstrap/
    healthcheck/job.yaml
  hooks/
    preflight.sh
    bootstrap.sh
    healthcheck.sh
```

The directory is the source for materializing the package. In prebuilt artifact mode, CI builds from this directory, pushes the package to a registry, and records the artifact digest in the release BOM. In source-first local build mode, an agent can build from the same source facts without relying on a remote OCI artifact.

## BOM Layout

Each release revision should have one BOM file:

```text
releases/default-platform/rev-20240424-prod/bom.yaml
```

The BOM should pin the immutable source and build contract for each package. When a prebuilt artifact is available, the same entry can also pin the artifact image and digest:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: default-platform-production
spec:
  revision: rev-20240424-prod
  packages:
    - category: infra
      name: kubernetes
      version: v1.30.3
      source:
        path: packages/infra/kubernetes/v1.30.3
        digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
      build:
        class: rootfs-image/v1
        profile: prod-amd64
      artifact:
        name: kubernetes-production-rootfs
        image: registry.example.io/sealos/kubernetes-production-rootfs:v1.30.3
        digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
        optional: true
      required: true
```

This makes release review focused on exactly which source revision, build contract, and optional prebuilt package digest are entering a distribution revision.

A BOM should not encode the channel that currently points to it. Channel membership is mutable release intent, while the BOM revision is immutable release content.

When both `source` and `artifact` are present, the artifact must be treated as a materialized result of the pinned source and build contract, not as a separate source of truth.

## Channel Layout

Channels should be tiny pointer files, not duplicated BOMs:

```text
channels/default-platform/beta.yaml
```

Example:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ReleaseChannel
metadata:
  name: beta
spec:
  distribution: default-platform
  targetRevision: rev-20240424-prod
  bomPath: releases/default-platform/rev-20240424-prod/bom.yaml
```

Promotion from `alpha` to `beta` or `beta` to `stable` then becomes a small pull request that changes only the channel target. That keeps promotion review separate from package build review.

## Boundary With Cluster Configuration

`distribution-config` and `cluster-config` should be separate Git repositories:

| Repository | Owner | Contents |
| --- | --- | --- |
| `distribution-config` | platform team | package sources, build classes, profiles, BOMs, channels, shared validation policy |
| `cluster-config` | cluster or environment owner | cluster targets, private inputs, local patches, non-exported environment configuration |

This document only defines the `distribution-config` repository. The companion [cluster configuration proposal](cluster-config.md) defines the `cluster-config` repository, including `ClusterTarget`, delivery policy, local inputs, patches, and secret references.

The Sealos agent can clone or fetch both repositories, read cluster-local intent from `cluster-config`, then resolve distribution channels, BOMs, profiles, and package materialization data from `distribution-config`.

This preserves the existing global/local ownership boundary:

- global baseline: reviewed once and promoted through shared release channels
- local patch: kept near the cluster and allowed to differ by environment

Repository URLs, credentials, and default Git refs should be supplied by agent configuration or deployment bootstrap, not embedded in shared package definitions.

## Source And Artifact Fulfillment Modes

The repository model should support two primary package fulfillment modes and one operational convenience mode:

| Mode | Behavior |
| --- | --- |
| `artifact` | Pull the prebuilt package artifact by digest from OCI or another configured artifact store. |
| `localBuild` | Build the package locally from the pinned source facts and build contract. |
| `preferArtifact` | Pull the prebuilt artifact when it is available and allowed; otherwise fall back to local build. |

The modes share the same package source tree, build classes, profiles, BOMs, and channels. They differ only in how the agent materializes the package before render/apply.

The BOM should always identify the source facts for buildable packages:

```yaml
source:
  path: packages/infra/kubernetes/v1.30.3
  digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
build:
  class: rootfs-image/v1
  profile: prod-amd64
artifact:
  image: registry.example.io/sealos/kubernetes-production-rootfs:v1.30.3
  digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
  optional: true
```

`source.digest` should be a deterministic digest of the normalized source facts used by the build. `artifact.digest` is required for `artifact` mode and optional for `localBuild` mode. If present, `artifact.digest` is a cache or distribution handle for the pinned source and build contract.

Delivery policy can be set by a distribution profile default and selected or overridden by `cluster-config` when policy allows:

```yaml
delivery:
  mode: artifact
```

Changing delivery mode must not change the package graph, feature resolution, profile defaults, input merge order, or patch order. If local build and prebuilt artifact fulfillment produce different desired state for the same BOM, that is a validation failure.

## Distribution Resolution Contract

Given a distribution, channel, profile, and delivery mode selected by `cluster-config`, an agent or operator should resolve distribution content in a deterministic order:

1. Resolve the selected channel to `channels/<distribution>/<channel>.yaml`.
2. Resolve the channel's `targetRevision` and `bomPath` to one BOM file.
3. Verify that the BOM `spec.revision` matches the channel `targetRevision`.
4. Resolve the selected profile under `profiles/<distribution>/<profile>/`.
5. Verify referenced build classes and package source paths.
6. Materialize each package by pulling a pinned artifact, building from pinned source facts, or using `preferArtifact` fallback rules.
7. Expose package defaults, profile defaults, and materialized package payloads to the cluster render/apply workflow.

Resolution should fail closed if a referenced file is missing, a required source digest is missing, an artifact digest does not match the pulled artifact, a local build cannot prove its source digest, an artifact is required but unavailable, or a required secret value is unavailable.

## Synchronization Flow

Recommended Day 0 and Day N flow:

1. A package author updates `packages/<category>/<name>/<version>/`.
2. CI validates `package.yaml`, computes the source digest, and checks the build contract.
3. In prebuilt artifact mode, CI builds the package artifact and pushes it to the registry. In source-first local build mode, CI may only validate or test-build the source facts.
4. Release automation writes or updates a BOM under `releases/<distribution>/<revision>/` with source digests and optional artifact digests.
5. Reviewers approve the BOM with digest-pinned source and artifact references.
6. Promotion updates a channel pointer under `channels/<distribution>/`.
7. The cluster agent pulls Git changes from both repositories, reads cluster-local intent from `cluster-config`, resolves the selected channel and BOM from `distribution-config`, materializes packages according to delivery policy, and runs render/apply.

The important split is that Git synchronizes source facts and release intent. OCI synchronizes optional prebuilt package content.

## Generated Output

Rendered bundles should not be committed by default. They are deterministic build output from:

- selected channel or BOM revision
- package source facts and materialized package payloads
- distribution profile defaults and masks
- cluster-local inputs and patches from `cluster-config`

Generated output belongs in the agent workspace, local cache, local registry, OCI layout, or CI artifacts. A rendered bundle or locally built package may be attached to a release for debugging, but it should not become the primary source of truth.

## Secret Handling

Secrets should not be stored in the distribution repository.

Allowed patterns:

- package manifests may declare required secret-shaped inputs
- secret values and cluster-specific secret references belong in `cluster-config` or runtime secret stores
- sensitive values should be injected from an in-cluster secret store during hydration
- certificates and private keys should stay outside package artifacts and be supplied as local inputs

The distribution repository should define the need for a secret, not the value or cluster-local binding.

## Validation

CI for the distribution configuration repository should validate:

- every `package.yaml` parses as `ComponentPackage`
- package paths referenced by `package.yaml` exist
- every BOM parses as `BOM`
- every package identity uses a valid `category`, `name`, and `version`
- every `category/name` tuple is unique
- every buildable BOM package points to a source path and source digest
- every buildable BOM package names a supported build class
- every `artifact` mode package points to an image and digest
- every digest is well-formed and no mutable tag is accepted without a digest pin
- every BOM package has a matching package source or approved external artifact
- every channel target revision matches the referenced BOM revision
- every channel pointer references an existing BOM path
- every profile references existing defaults, masks, full package identities, and supported features
- every build class reference points to an approved class definition
- delivery mode changes do not change resolved package graphs or patch ordering
- generated output paths and local caches are ignored by Git

This validation can start as repository-local scripts and later move into first-class `sealos sync` commands.

## Alternatives Considered

### Store Everything In One Flat Directory

Rejected because package source, release intent, channel promotion, and cluster-local patches have different owners and review cycles.

### Store Built Packages Directly In Git

Rejected because Git is a poor transport for large immutable payloads. OCI already provides digest-addressed artifact distribution and local caching.

### Store One BOM Per Channel

Rejected because it duplicates release definitions. Channel files should point to BOM revisions so promotion remains a small metadata change.

### Put Cluster Overrides Inside Component Directories

Rejected because that mixes global package ownership with environment-specific configuration and makes package promotion unsafe.

## Open Questions

- Should Sealos define a first-class `ReleaseChannel` schema, or should channel pointers remain a repository convention at first?
- How should `source.digest` be computed for a package source tree so it remains deterministic across platforms?
- How should validation discover externally produced package artifacts that do not have source under `packages/`?
- Should rendered bundle snapshots be allowed in a dedicated audit repository for regulated environments?

## Recommendation

Start with the proposed `docs/projects` structure for design documents and use the repository layout above for distribution configuration repositories.

For new work, prefer:

```text
packages/<category>/<name>/<version>/package.yaml
profiles/<distribution>/<profile>/defaults.yaml
releases/<distribution>/<revision>/bom.yaml
channels/<distribution>/<channel>.yaml
```

This gives platform teams stable paths for review, automation, promotion, and pull-based cluster synchronization without changing the existing OCI package and BOM model. Cluster-local paths are defined in the companion [cluster configuration proposal](cluster-config.md).
