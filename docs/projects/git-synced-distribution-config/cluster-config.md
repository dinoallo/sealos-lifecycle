# Proposal: Git-Synced Cluster Configuration

## Status

Draft

## Summary

This document defines a separate `cluster-config` Git repository layout for cluster-local Sealos lifecycle configuration.

The [distribution configuration proposal](proposal.md) defines platform distribution facts: package sources, build classes, profiles, BOMs, channels, and shared validation policy. The `cluster-config` repository owns cluster-local intent: `ClusterTarget`, delivery policy selection, non-secret inputs, local patches, and secret references.

Keeping these repositories separate makes ownership, access control, and promotion safer. Platform teams can promote global release content without seeing cluster-private values, while cluster owners can change local inputs without editing shared distribution definitions.

For the shared catalog of document kinds, see the [document kind reference](kinds.md).

## Goals

- Provide one stable entrypoint for each cluster.
- Keep cluster-local inputs and patches out of `distribution-config`.
- Allow cluster owners to choose distribution, channel, profile, and delivery mode within platform policy.
- Support pull-based synchronization from private clusters.
- Keep secret values out of Git by default.
- Make the files affecting one cluster easy to find and validate.

## Non-Goals

- Defining the `distribution-config` repository layout.
- Replacing the release BOM or channel model.
- Storing rendered bundles or built package artifacts as source of truth.
- Storing plaintext private keys, tokens, certificates, or secret values.
- Defining repository hosting, authentication, or branch protection requirements.

## Recommended Repository Model

Use a separate cluster configuration repository for environment-owned content:

```text
cluster-config/
  clusters/
    prod/
      prod-a/
        target.yaml
        inputs/
          kubernetes.yaml
          cilium.yaml
        patches/
          kube-apiserver-audit.yaml
        secrets/
          refs.yaml
    staging/
      staging-a/
        target.yaml
        inputs/
        patches/
  policy/
    validation/
  README.md
```

The repository should contain cluster-local source configuration. It should not contain generated render output, local caches, downloaded package artifacts, or built package payloads.

## Directory Responsibilities

| Path | Responsibility |
| --- | --- |
| `clusters/<scope>/<cluster>/target.yaml` | The cluster's stable entrypoint for distribution, channel, profile, delivery mode, inputs, and patches. |
| `clusters/<scope>/<cluster>/inputs/` | Non-secret cluster-specific package inputs. |
| `clusters/<scope>/<cluster>/patches/` | Cluster-local overlays or structured patches. |
| `clusters/<scope>/<cluster>/secrets/` | Secret references or encrypted/sealed secret material when allowed by the operating model. |
| `policy/` | Validation rules used by CI or agent-side preflight checks. |

## Cluster Target

Each cluster should have exactly one `target.yaml`.

Example:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ClusterTarget
metadata:
  name: prod-a
spec:
  distribution: default-platform
  channel: stable
  profile: prod-amd64
  delivery:
    mode: preferArtifact
  distributionRef:
    name: platform
    ref: main
  localPatchRevision: prod-a-20240425
  inputs:
    - component: kubernetes
      path: inputs/kubernetes.yaml
    - component: cilium
      path: inputs/cilium.yaml
  patches:
    - path: patches/kube-apiserver-audit.yaml
  secrets:
    - path: secrets/refs.yaml
```

The target file should not duplicate BOM contents. It selects a distribution, channel, and profile, then points to local inputs and patches that are applied after distribution defaults.

Repository URLs and credentials should be supplied by agent configuration or deployment bootstrap. `distributionRef` may name a configured distribution repository and Git ref, but it should not embed credentials.

## Delivery Policy

The cluster target can select how packages are materialized when policy allows:

| Mode | Behavior |
| --- | --- |
| `artifact` | Pull the prebuilt package artifact by digest from OCI or another configured artifact store. |
| `localBuild` | Build the package locally from the source facts and build contract pinned by the selected BOM. |
| `preferArtifact` | Pull the prebuilt artifact when available and allowed; otherwise fall back to local build. |

Changing delivery mode must not change the selected distribution revision, package graph, feature resolution, input merge order, or patch order. It only changes how the package payload is materialized before render/apply.

## Inputs And Patches

Inputs should be small, non-secret YAML documents scoped to one component.

Example input:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentInput
metadata:
  name: prod-a-kubernetes
spec:
  component: kubernetes
  values:
    clusterCIDR: 10.244.0.0/16
    serviceCIDR: 10.96.0.0/12
    controlPlaneEndpoint: api.prod-a.example.com
```

Patches should be explicit overlays or structured patch documents. They should not rely on implicit filesystem discovery outside the cluster directory.

Paths in `target.yaml` are relative to the cluster root, for example `clusters/prod/prod-a/`. Absolute paths and `..` traversal should be rejected.

## Secret Handling

Secret values should not be stored in plaintext Git files.

Allowed patterns:

- store only secret references in Git
- keep sealed or encrypted secrets in the private cluster repository if the operating model supports it
- inject sensitive values from an in-cluster secret store during hydration
- keep certificates and private keys outside package artifacts and provide them as local inputs at runtime

The distribution package manifest may declare required secret-shaped inputs. `cluster-config` binds those requirements to local secret references or runtime injection points.

## Resolution Contract

An agent or operator should resolve a cluster in a deterministic order:

1. Read the cluster's `target.yaml`.
2. Resolve the configured `distribution-config` repository and Git ref.
3. Resolve the selected distribution channel to one BOM.
4. Resolve the selected distribution profile.
5. Materialize packages according to the selected delivery mode.
6. Merge package defaults, profile defaults, cluster inputs, and cluster patches in a documented order.
7. Inject required secrets from approved local sources.
8. Render and apply, or write generated output to a local workspace or CI artifact.

Resolution should fail closed if a referenced file is missing, a local path escapes the cluster root, a selected channel/profile does not exist, a delivery mode is not allowed, a required secret binding is missing, or a patch cannot be applied cleanly.

## Validation

CI or agent preflight checks for the cluster configuration repository should validate:

- every `target.yaml` parses as `ClusterTarget`
- every cluster target selects an allowed distribution, channel, profile, and delivery mode
- every referenced input, patch, and secret reference path exists
- local paths are relative and do not escape the cluster root
- non-secret input files do not contain obvious secret material
- patches apply cleanly against the selected distribution profile and package defaults
- local build mode is allowed only for clusters with the required build capability
- generated output paths and local caches are ignored by Git

## Recommendation

Keep all cluster-local state in `cluster-config`:

```text
clusters/<scope>/<cluster>/target.yaml
clusters/<scope>/<cluster>/inputs/
clusters/<scope>/<cluster>/patches/
clusters/<scope>/<cluster>/secrets/
```

Keep platform-owned distribution state in `distribution-config`. The agent joins the two repositories at resolution time.
