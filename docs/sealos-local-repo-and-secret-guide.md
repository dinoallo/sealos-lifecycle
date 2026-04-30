# Guide: Local Repo Layout And Secret Initialization

## Status

Design guide

## Summary

This guide explains what the cluster-local repo should logically contain, how it
relates to `spec.inputs`, and what the correct secret-initialization workflow
should look like.

It is intentionally a design guide, not a description of a fully implemented
code path. The current repository still does not contain a finished
`pkg/distribution/localrepo` package, so the layout below should be read as the
recommended direction for the MVP and beyond.

## Related Documents

- Top-level distribution model:
  [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md)
- Ownership and reconcile model:
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- Package contract and `spec.inputs`:
  [sealos-component-package-format-design.md](./sealos-component-package-format-design.md)
- Grafana and database example:
  [sealos-grafana-kubeblocks-example.md](./sealos-grafana-kubeblocks-example.md)
- BOM and `DistributionChannel` guide:
  [sealos-bom-and-distribution-channel-guide.md](./sealos-bom-and-distribution-channel-guide.md)

## What The Local Repo Is

The local repo is the cluster-local source of truth for data that must not live
inside the shared package artifacts.

Its job is to hold:

- concrete values for declared package inputs
- local-only Secret material or Secret references
- allowed local-owned resources
- cluster-local revision metadata or bookkeeping

Its job is not to:

- replace the BOM
- replace package artifacts
- act as a blanket override layer for arbitrary package internals
- persist runtime-generated state as if it were baseline input

The key boundary rule remains:

- packages define the reusable global contract
- the local repo binds cluster-specific values into that contract

## What Belongs In The Local Repo

| Category | Examples | Why It Belongs Here |
| --- | --- | --- |
| Declared input payloads | CIDR values, endpoint overrides, values files, config fragments | These are cluster-specific bindings for package inputs. |
| Secret names and secret-bearing resources | Grafana admin Secret, database root Secret, TLS Secret manifests or references | Secret bytes must remain cluster-local. |
| Explicit local-owned resources | Local overlays, environment-specific ingress objects, allowed namespace-scoped policy tweaks | These are not part of the shared package baseline. |
| Local revision metadata | Local patch revision id, local repo revision hash | Needed for drift comparison and audit. |

## What Does Not Belong In The Local Repo

The local repo should not contain:

- copied BOM snapshots that pretend to be local input
- unpacked package baselines edited in place
- runtime-generated database passwords exported back from the cluster
- arbitrary direct overrides against global-owned package content

If a change needs to be shared across many clusters, it should move into:

- the package baseline
- a shared patch package
- or a derived distribution line

## Recommended Logical Layout

One reasonable first-pass layout is:

```text
local-repo/
  repo.yaml
  revisions/
    current.yaml
  inputs/
    grafana/
      grafana-values.yaml
    grafana-db/
      grafana-db-values.yaml
  resources/
    secrets/
      grafana-admin-credentials.yaml
      grafana-db-root.yaml
    external-secrets/
      grafana-admin-credentials.external-secret.yaml
      grafana-db-root.external-secret.yaml
  patches/
    grafana/
      ingress.patch.yaml
```

Important notes:

- `inputs/` holds payloads that bind to declared `spec.inputs`
- `resources/` holds local-owned Kubernetes objects, especially Secret-bearing
  resources
- `patches/` is only for allowed local-owned surfaces, not arbitrary mutation of
  package baseline intent
- `repo.yaml` and `revisions/current.yaml` are illustrative metadata files, not
  fixed schema commitments yet

## Recommended Metadata Files

The local repo should eventually have a small metadata file that says what this
repo instance is for.

For example:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepo
metadata:
  name: poc-minimal-local
spec:
  clusterName: poc-minimal
  line: default-platform
```

And one revision bookkeeping file such as:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepoRevision
metadata:
  name: current
spec:
  revision: local-20260501-001
  inputsHash: sha256:<hash>
```

These objects are not implemented yet, but the model is useful:

- one document identifies the local repo
- another identifies the current cluster-local input revision

## How `spec.inputs` Maps Into The Local Repo

If a package declares:

```yaml
inputs:
  - name: grafana-values
    type: valuesFile
    path: files/values/basic.yaml
```

the package is saying:

- there is one cluster-local binding surface called `grafana-values`
- the package baseline carries its own default file at
  `files/values/basic.yaml`
- the cluster may bind its own concrete values into that surface during
  hydration

The local repo should then provide the concrete cluster payload, for example:

```text
local-repo/
  inputs/
    grafana/
      grafana-values.yaml
```

The exact filename convention is still design territory, but the semantic rule
should be stable:

- package input name or component name should map deterministically to one local
  payload location

## Recommended Secret Handling Model

For secrets, use a two-layer pattern:

1. Put the secret reference or secret name in the input payload.
2. Put the actual secret bytes in a cluster-local secret resource or secret
   system.

That means packages can stay reproducible while the local repo remains the
cluster-local authority for secret material.

### Example: Grafana Database

Input payload:

```yaml
clusterName: grafana-db
systemAccounts:
  postgres:
    secretName: grafana-db-root
```

Local secret resource:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: grafana-db-root
type: Opaque
stringData:
  username: postgres
  password: <cluster-local-password>
```

The input payload is a binding contract. The Secret object is the secret source.

## Two Correct Initialization Paths

There are two sane secret-initialization patterns.

### Path A: Direct Local Secret Manifest

This is the simplest bootstrap path and the easiest MVP shape.

The local repo contains a Secret manifest directly:

```text
local-repo/
  inputs/
    grafana/
      grafana-values.yaml
  resources/
    secrets/
      grafana-admin-credentials.yaml
```

Use this when:

- the environment is small or self-contained
- you need a low-friction bootstrap path
- the local repo is not being synchronized to a shared remote service

This is acceptable for:

- labs
- small private environments
- the first MVP

But it should not be the only long-term answer for production environments.

### Path B: Cluster-Local Secret Reference

This is the preferred long-term production pattern.

The local repo contains a reference resource, not the raw Secret bytes. For
example:

- an `ExternalSecret`
- a `SecretProviderClass`
- a SOPS-encrypted Secret manifest
- another cluster-local secret-manager reference object

Illustrative example:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: grafana-admin-credentials
spec:
  secretStoreRef:
    name: cluster-vault
    kind: ClusterSecretStore
  target:
    name: grafana-admin-credentials
  data:
    - secretKey: admin-user
      remoteRef:
        key: grafana/admin-user
    - secretKey: admin-password
      remoteRef:
        key: grafana/admin-password
```

Use this when:

- the environment already has a secret manager
- Git-backed local repo storage must not hold raw secret bytes
- rotation and audit need to be handled by dedicated secret systems

## Recommended Initialization Workflow

The correct operator workflow should be:

1. Choose the BOM revision or the `distribution line + DistributionChannel`.
2. Materialize or create the local repo skeleton for the cluster.
3. Fill in non-secret input values under `inputs/`.
4. Create the required secret resources or secret references under
   `resources/`.
5. Validate that every required package input and every required secret
   reference exists before hydrate proceeds.
6. Hydrate the desired state from `BOM + local repo`.
7. Apply secret resources first when they are part of the local-owned resource
   set, then apply dependent package content.

The key point is:

- the secret should exist before the dependent workload is expected to become
  healthy

## What Should Not Happen

The wrong initialization patterns are:

- editing the package baseline file and embedding the secret bytes there
- putting the secret bytes into the BOM
- letting local repo act as a free-form overlay against unrelated package paths
- exporting runtime-generated secrets back into shared package content

Those behaviors break reproducibility, ownership, or both.

## Secret Initialization For Day 0

On Day 0, secret initialization should be treated as a prerequisite for any
package that depends on it.

For example:

- `grafana-db` needs `grafana-db-root`
- `grafana` needs `grafana-admin-credentials`

The bootstrap sequence should therefore be:

1. initialize or reference the required local Secret objects
2. apply the database package
3. wait until the database is ready or its generated credentials exist
4. apply the dependent application package

This is one reason database and application packages should remain split.

## Runtime-Generated Secret Boundary

The local repo should initialize cluster-owned secrets that must exist before
bootstrap.

It should not try to own every Secret that appears later at runtime.

Examples that should remain runtime-local:

- KubeBlocks-generated account Secret contents
- one-time bootstrap tokens generated by operators
- auto-generated internal app secrets unless Sealos deliberately chooses to
  externalize and manage them

Those may still be observed or referenced, but they should not be pushed back
into the shared baseline or blindly copied into the local repo as if they were
input.

## Practical Rule Of Thumb

If a package needs a secret:

- declare the binding surface in `spec.inputs`
- store the secret name or reference in the local input payload
- initialize the actual secret bytes through a local Secret object or local
  secret system
- validate that the secret exists before expecting the package to converge

That is the cleanest boundary between package baseline, local repo, and runtime
state.
