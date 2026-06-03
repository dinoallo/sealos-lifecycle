# Guide: Local Repo Layout And Secret Initialization

## Status

Design guide with current single-node MVP notes

## Summary

This guide explains what the cluster-local repo should logically contain, how it
relates to `spec.inputs`, and what the correct secret-initialization workflow
should look like.

It is intentionally still a design guide, not a statement that every part of
the end-state model is already complete. The current repository now has a
working single-node `pkg/distribution/localrepo` MVP with `inputs/`,
`resources/`, and `patches/`, but the broader layout below should still be read
as the recommended direction for the MVP and beyond.

## Related Documents

- Top-level distribution model:
  [Distribution and config sync](../architecture/distribution-and-config-sync.md)
- Ownership and reconcile model:
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- Tracking model for rendered files, objects, and generated outputs:
  [Materialization and drift](../architecture/materialization-and-drift.md)
- Local patch policy source, scope, and provenance:
  [Local patch policy](../architecture/local-patch-policy.md)
- Package contract and `spec.inputs`:
  [Package format](../architecture/package-format.md)
- Grafana and database example:
  [Grafana with KubeBlocks](../guides/grafana-kubeblocks-example.md)
- BOM and `ReleaseChannel` guide:
  [BOM and channel](../guides/bom-and-channel.md)

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
  policy/
    local-patch-policy.yaml
  inputs/
    grafana/
      grafana-values.yaml
    grafana-db/
      grafana-db-values.yaml
    kubernetes/
      kubeadm-config.yaml
      hosts/
        192.168.0.240/
          kubeadm-config.yaml
        192.168.0.238/
          kubeadm-config.yaml
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
- for multi-node modeling, `inputs/<component>/hosts/<host>/...` is also a
  supported directory convention for host-scoped input provenance
- `policy/local-patch-policy.yaml` is an optional explicit policy artifact for
  the current single-node MVP. When present, render copies it into the bundle
  and the same rendered policy is then consumed by local-patch validation,
  drift comparison, and `sync commit`
- `resources/` holds local-owned Kubernetes objects, especially Secret-bearing
  resources
- in the current single-node MVP, `resources/` keeps its relative directory
  structure when rendered into the bundle, for example
  `resources/secrets/grafana-admin-credentials.yaml` becomes
  `local-resources/secrets/grafana-admin-credentials.yaml`
- `patches/` is only for allowed local-owned surfaces, not arbitrary mutation of
  package baseline intent
- in the current single-node MVP, `patches/` is component-scoped:
  `patches/<component>/**/*.yaml`
- each patch document is a partial Kubernetes object overlay identified by
  `apiVersion`, `kind`, `metadata.name`, and usually `metadata.namespace`
- these patch documents are merged into matching package manifest objects during
  render; they are not applied as standalone live resources
- the current ownership validator allows a narrow set of local patch surfaces:
  it is now represented as a schema-backed `LocalPatchPolicy` artifact, not
  just an ad hoc allowlist

Current boundary for host-scoped inputs:

- render and the bundle now preserve host-scoped bindings as explicit
  provenance through `hostInputBindings`
- these `hostInputBindings` entries now point at rendered bundle-local copies
  under `components/<component>/host-inputs/<host>/...`, so the bundle remains
  self-describing even when the original local repo path is unavailable
- the default `inputs/<component>/<file>` payload remains the only binding that
  is automatically overlaid during render today
- multi-node `sync apply` consumes host-scoped input payloads for
  local-input-backed direct `file` content; other content types still use the
  default rendered payload
- `sync commit --host <host>` uses the same provenance: if the selected host
  has a `hostInputBindings` entry, commit writes the live file back to
  `inputs/<component>/hosts/<host>/<input-file>` and to the matching
  `components/<component>/host-inputs/<host>/...` bundle copy; it does not
  overwrite the default input for other hosts
- if a selected host has no host-scoped input and multiple hosts have
  different live contents, commit refuses to write the selected host's value
  into the default input; initialize the host-scoped input first or make the
  desired value common to every host
- `sync diff/status` exposes this provenance in tracked host-path summaries:
  host-scoped matches show `usesHostScopedInput` and `hostInputBindingPath`,
  while split summaries list which divergent hosts already have scoped payloads
  and which do not
  `ConfigMap.data`, `ConfigMap.binaryData`, workload placement fields such as
  `nodeSelector` / `tolerations` / `affinity`, selected secret-name bindings,
  and ingress or service exposure fields such as `spec.rules`, `spec.tls`, and
  selected metadata annotations
- if `local-repo/policy/local-patch-policy.yaml` is absent, render still writes
  an explicit policy artifact into the bundle, either from the selected BOM,
  from exactly one selected package, or from the built-in default
- the current ownership model is therefore explicit:
  package and BOM content may select the effective cluster-local policy, but
  they do not create a package/BOM-scoped policy surface
  the rendered bundle will mark policy provenance as one of:
  `localPatchPolicySource: localRepo`
  `localPatchPolicySource: bom`
  `localPatchPolicySource: package`
  `localPatchPolicySource: builtInDefault`
- the policy artifact itself now also carries `spec.scope: clusterLocal`
  regardless of source; package/BOM-scoped local-patch policy and multi-layer
  policy merge are currently unsupported
- if a bundle claims any other source, or its recorded policy name/path/digest
  do not match the rendered artifact, current policy consumers reject that
  bundle instead of guessing
- the current single-node MVP now supports `sealos sync commit --local-repo ...`
  for one narrow path only:
  it can persist `Dirty` live drift back into existing `patches/` files when
  that drift is backed by a tracked `localPatch` fragment
- it can also persist `Dirty` drift for standalone local-owned resource objects
  back into their original `resources/` files
- it can also persist `Dirty` drift for a tracked local-owned host file when
  that host file is backed by a declared local input binding; in the current
  MVP this is limited to regular files and writes the live file content back to
  the bound default or host-scoped `inputs/` payload plus the rendered bundle
  copy
- it still does not commit `Orphan` drift, mixed package-plus-resource objects,
  symlink-based local host paths, or arbitrary input changes back into the
  local repo
- in current `sync diff` / `sync status` output, this local repo split is now
  also visible as remediation ownership:
  `changeOwner=localOverlay` usually points back to `patches/` or
  `resources/`, while `changeOwner=localInput` points back to an `inputs/`
  payload that bound a direct host-side file
- the same `sync diff` / `sync status` output now also exposes a top-level
  `localPatchPolicy` block so operators can see the effective policy source,
  name, path, and digest for the rendered bundle they are inspecting
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

1. Choose the BOM revision or the `distribution line + ReleaseChannel`.
2. Initialize the local repo skeleton for the cluster from the BOM and package
   input contracts.
3. Fill in non-secret input values under `inputs/`.
4. Create the required secret resources or secret references under
   `resources/`.
5. Validate that every required package input and every required secret
   reference exists before hydrate proceeds.
6. Hydrate the desired state from `BOM + local repo`.
7. Preview the rendered apply intent with `sync plan`, including target
   resolution, local resources, and Secret object summaries.
8. Apply secret resources first when they are part of the local-owned resource
   set, then apply dependent package content.

Current CLI initialization entry:

```bash
sealos sync local-repo init \
  --file bom.yaml \
  --package-source grafana=./packages/grafana \
  --package-source grafana-db=./packages/grafana-db \
  --output-dir ./local-repo \
  --output yaml
```

The initializer creates `inputs/` templates, `resources/` and `patches/`
directories, `policy/local-patch-policy.yaml`, and minimal local repo metadata.
It does not generate real Secret bytes. Secret-like inputs are written with
private file permissions and are also reported as hints so operators can create
the corresponding Secret manifest or external-secret reference under
`resources/`.

After operators fill the local repo, run the local repo doctor before the
broader validation step:

```bash
sealos sync local-repo doctor \
  --file bom.yaml \
  --package-source grafana=./packages/grafana \
  --package-source grafana-db=./packages/grafana-db \
  --local-repo ./local-repo \
  --output yaml
```

The doctor is also read-only, but it is focused on the local repo itself. It
reports unresolved `local-repo init` placeholders, missing required inputs,
stale component directories under `inputs/` or `patches/`, missing
`policy/local-patch-policy.yaml`, non-manifest files under `resources/`, and
Secret-like files whose kind or file mode is unsafe. It reports file paths and
fix suggestions only; it does not print input or Secret payload content.

For CI or a one-command operator gate before render, use source preflight:

```bash
sealos sync preflight \
  --cluster default \
  --file bom.yaml \
  --package-source grafana=./packages/grafana \
  --package-source grafana-db=./packages/grafana-db \
  --local-repo ./local-repo \
  --output yaml
```

With `--file`, preflight runs the local-repo doctor when `--local-repo` is set
and then runs the broader `sync validate` contract check. A passing result
includes the exact `sealos sync render ...` command to run next. Without
`--file`, the same `sync preflight` command keeps its rendered-bundle mode and
checks whether `--bundle-dir` would pass apply gates. That rendered-bundle mode
checks topology/render-input freshness and local runtime readiness, including
host mutation privileges, systemd availability, swap, existing Kubernetes node
state, bootstrap ports, known runtime binaries, kubeconfig/client availability,
and managed service state. Runtime warnings stay in structured output under
`runtimeStatus`; blocking runtime checks stop `sync apply`.

`sealos sync render` now runs the same source preflight by default before it
materializes a bundle. Blocking source issues stop render and are returned in
the structured `sourcePreflight` output. Use `--skip-source-preflight` only for
development or debugging when you intentionally want to render from incomplete
or unsafe source inputs. A successful render also persists a sanitized
`spec.sourcePreflight` summary into the rendered bundle. That summary records
only state, blocked reasons, aggregate counts, and the doctor/validate stage
results; it does not copy input or Secret payload content.

Current CLI validation entry:

```bash
sealos sync validate \
  --cluster default \
  --file bom.yaml \
  --package-source grafana=./packages/grafana \
  --package-source grafana-db=./packages/grafana-db \
  --local-repo ./local-repo \
  --output yaml
```

The validator is intentionally read-only. It checks the BOM/package/local-repo
contract before render/apply, including package source validity, required input
bindings, host-scoped input hosts against the current cluster inventory,
local patch policy compatibility, target resolvability, and obvious Secret
manifest file permission mistakes. For tests and scripted smoke runs, add
`--runtime-root <dir>` to point validation at a specific Clusterfile inventory.

After render, use `sync plan` as the read-only operator review step:

```bash
sealos sync plan \
  --cluster default \
  --bundle-dir <rendered-bundle-dir> \
  --output yaml
```

The plan output resolves `allNodes`, `firstMaster`, and `cluster` targets,
summarizes component steps, local resources, tracked Kubernetes objects, and
tracked host paths. Secret objects are reported as sensitive object summaries;
the command does not print Secret payload fields such as `data` or
`stringData`. If an older bundle has no `spec.sourcePreflight` metadata,
`sync plan`, `sync apply`, `sync diff`, and `sync status` include a warning so
operators know the source readiness result was not recorded at render time.
`sync diff` and `sync status` also expose the recorded `sourcePreflight`
summary next to the live drift summary, which lets operators correlate live
changes with the source checks that produced the rendered bundle.

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
