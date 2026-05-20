# Example: Minimal `sealos sync` Drift Scenario

## Status

Reference example for the current single-node MVP

## Summary

This directory gives a minimal local file layout that matches the current
single-node `sealos sync` drift workflow.

It is not a fully self-contained cluster fixture. It is a concrete example of:

- what a small local repo can look like
- which files normally back `localOverlay` vs `localInput`
- what command order to use when inspecting, committing, or reverting drift

Use it together with:

- a real rendered bundle directory
- a real cluster kubeconfig
- a real `AppliedRevision`

It now also includes schema-aligned example fixtures under:

- `bundle/bundle.yaml`
- `bundle/components/...`
- `bundle/local-resources/...`
- `bundle/policy/local-patch-policy.yaml`
- `applied-revision.example.yaml`
- `policy-gate-approved.example.yaml`
- `sync-diff.example.yaml`
- `sync-status.example.yaml`

These fixtures are designed to match the current `sync` drift model, not to
pretend every path is directly runnable without adjustment.

## Directory Layout

```text
docs/examples/sync-drift-minimal/
  README.md
  README.zh-CN.md
  applied-revision.example.yaml
  policy-gate-approved.example.yaml
  sync-diff.example.yaml
  sync-status.example.yaml
  bundle/
    bundle.yaml
    components/
    local-resources/
    policy/
      local-patch-policy.yaml
  local-repo/
    inputs/
      kubernetes/
        kubeadm-cluster-config.yaml
    policy/
      local-patch-policy-approval.approved-example.yaml
      local-patch-policy.yaml
      local-patch-policy-approval.yaml
    resources/
      secrets/
        grafana-admin-credentials.yaml
    patches/
      grafana/
        grafana-settings.patch.yaml
```

What each file is meant to represent:

- `inputs/kubernetes/kubeadm-cluster-config.yaml`
  - a local input payload that can back a direct host-side file such as
    `/etc/kubernetes/kubeadm.yaml`
  - in current remediation output, this is the kind of source that maps to
    `changeOwner=localInput`
- `resources/secrets/grafana-admin-credentials.yaml`
  - a standalone local-owned Kubernetes object
  - in current remediation output, this is part of the `localOverlay` side
- `patches/grafana/grafana-settings.patch.yaml`
  - a local overlay document against a package-provided `ConfigMap`
  - in current remediation output, this is also `localOverlay`
- `policy/local-patch-policy.yaml`
  - an explicit cluster-local policy artifact for local patch validation
  - in current rendered output, this is what now surfaces as the top-level
    `localPatchPolicy` provenance block
- `policy/local-patch-policy-approval.yaml`
  - an auditable exception file for `sealos sync policy-gate`
  - it now binds both the compared old policy and candidate new policy by
    `name`, `scope`, and `digest`
  - it also carries lifecycle metadata:
    `owner`, `approvedBy`, `changeRef`, and `expiresAt`
  - if a violation is approved, the file must also pin the intended
    `expectedCount` and `expectedImpact`
  - in this example it is intentionally empty, so the example stays on the
    default strict gate behavior
- `policy/local-patch-policy-approval.approved-example.yaml`
  - a non-empty example approval file that shows what an auditable exception
    looks like once specific widening or incompatible-patch impacts are
    intentionally accepted
  - unlike the default example approval file above, this one is only meant as a
    shape reference and pairs with `policy-gate-approved.example.yaml`

What the example fixtures are meant to represent:

- `bundle/bundle.yaml`
  - a rendered-bundle example with tracked objects, tracked host paths, and
    rendered component entries
- `bundle/components/grafana/local-patches/grafana-settings.patch.yaml`
  - the rendered copy of the local patch that backs `localOverlay`
- `bundle/local-resources/secrets/grafana-admin-credentials.yaml`
  - the rendered copy of the local-owned Secret
- `bundle/policy/local-patch-policy.yaml`
  - the rendered copy of the effective local patch policy carried by this
    bundle revision
- `bundle/components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml`
  - the rendered file that corresponds to the tracked local input-backed host
    path
- `applied-revision.example.yaml`
  - a schema-aligned recorded-state companion that matches the example bundle
    and shows what `AppliedRevision` looks like after an initial successful
    apply plus a later observed drift snapshot
- `sync-diff.example.yaml`
  - a shortened, schema-aligned `sealos sync diff` output snapshot for the same
    conceptual drift scenario
- `sync-status.example.yaml`
  - a shortened, schema-aligned `sealos sync status` output snapshot for the
    same conceptual drift scenario
- `policy-gate-approved.example.yaml`
  - a shortened, schema-aligned `sealos sync policy-gate` snapshot that shows a
    candidate policy change passing only because an approval file was provided
  - use it to see where `approvalSummary`, its lifecycle metadata, and
    `approvedViolations[].impact` surface in current CLI output
  - it also shows the current `followUpAction` hint that tells operators when
    the approval should be removed or renewed

## Recorded State Companion

This example directory now includes:

- `applied-revision.example.yaml`
- `sync-diff.example.yaml`
- `sync-status.example.yaml`
- `policy-gate-approved.example.yaml`

Use it together with:

- `bundle/bundle.yaml`
- `local-repo/...`

to understand the three recorded layers in the current single-node MVP:

- rendered desired state
- cluster-local source inputs and overlays
- recorded applied/observed cluster state
- operator-facing drift output snapshots

The digests and timestamps in `applied-revision.example.yaml` are illustrative.
They are meant to show schema shape and field relationships, not to claim that
the example bundle was generated with those exact values.

The same rule applies to policy provenance:

- `bundle/spec.localPatchPolicy*` shows how the current rendered bundle records
  the effective policy source, scope, name, path, and digest
- `bundle/policy/local-patch-policy.yaml` is the rendered artifact referenced
  by that metadata
- `local-repo/policy/local-patch-policy.yaml` is the cluster-local source
  artifact copied into the bundle in this example

The same rule also applies to `sync-diff.example.yaml` and
`sync-status.example.yaml`, plus `policy-gate-approved.example.yaml`: they are
shortened, schema-aligned output
snapshots, not a claim that every omitted field or count came from a literal
one-shot command run against this directory without any adjustment.

## Important Note About `inputBindings`

In a real rendered bundle, `components[].inputBindings` records the absolute
local-repo path that was used during render.

Because a documentation fixture cannot know your actual filesystem root, the
example bundle uses this placeholder:

```text
/ABSOLUTE/PATH/TO/local-repo/inputs/kubernetes/kubeadm-cluster-config.yaml
```

If you want to experiment with `sync commit` for the local input-backed host
file path, replace that placeholder with the real absolute path of:

```text
docs/examples/sync-drift-minimal/local-repo/inputs/kubernetes/kubeadm-cluster-config.yaml
```

## Files In This Example

### `local-repo/inputs/kubernetes/kubeadm-cluster-config.yaml`

This file stands in for a cluster-local bootstrap input:

```yaml
apiVersion: kubeadm.k8s.io/v1beta4
kind: ClusterConfiguration
clusterName: demo
networking:
  podSubnet: 10.244.0.0/16
  serviceSubnet: 10.96.0.0/12
```

In the current single-node MVP, a rendered bundle may project a file like this
into a tracked host path such as `/etc/kubernetes/kubeadm.yaml`.

### `local-repo/policy/local-patch-policy.yaml`

This file stands in for an explicit cluster-local local-patch policy:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: custom-local-patch-policy
spec:
  scope: clusterLocal
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
        - binaryData
```

In the current single-node MVP, this policy is:

- read from `local-repo/policy/local-patch-policy.yaml` during render
- copied into `bundle/policy/local-patch-policy.yaml`
- recorded in `bundle.yaml` as `localPatchPolicySource`,
  `localPatchPolicyScope`, `localPatchPolicyName`, `localPatchPolicyPath`, and
  `localPatchPolicyDigest`
- currently only `spec.scope: clusterLocal` is supported; package/BOM-scoped
  local-patch policy is intentionally unsupported in this MVP
- consumed later by local patch validation, compare-side `policyEligible`
  annotation, and `sync commit`

`localPatchPolicyDigest` is the digest of the rendered
`bundle/policy/local-patch-policy.yaml` artifact. The policy-gate approval
files bind policy identity with the normalized policy-document digest emitted by
`sync policy-gate`, so those example digest values are not interchangeable.

### `local-repo/resources/secrets/grafana-admin-credentials.yaml`

This file stands in for a local-owned Secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin-credentials
  namespace: default
type: Opaque
stringData:
  username: admin
  password: passw0rd
```

This is the current shape for local drift that may be:

- inspected through `sync diff` / `sync status`
- committed back into `resources/`
- or reverted back to the recorded desired state

### `local-repo/patches/grafana/grafana-settings.patch.yaml`

This file stands in for an allowed local patch:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-settings
  namespace: default
data:
  adminUser: root
```

This matches the current MVP patch contract:

- component-scoped under `patches/<component>/`
- partial Kubernetes object overlay
- restricted to allowed local-owned paths

## Minimal Command Order

Assume:

- bundle dir: `docs/examples/sync-drift-minimal/bundle`
- local repo: `docs/examples/sync-drift-minimal/local-repo`
- kubeconfig: `/etc/kubernetes/admin.conf`

This section only pins the exact command forms against this example directory.
For why a command is appropriate, how `operatorAction` is derived, or how
guardrails such as `bundleMatchesRecordedDesiredStateDigest` work, use:

- [sealos-sync-drift-walkthrough.md](../../sealos-sync-drift-walkthrough.md)
- [sealos-sync-operator-action-reference.md](../../sealos-sync-operator-action-reference.md)
- `sync-diff.example.yaml`
- `sync-status.example.yaml`

### 1. Inspect The Raw Drift

```bash
sealos sync diff \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

### 2. Inspect The Ownership Summary

```bash
sealos sync status \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

### 3. Commit Intentional Local Drift

Use `commit` only when the remediation points to:

- `changeOwner=localOverlay`
- or `changeOwner=localInput`

and the command is currently `available`.

```bash
sealos sync commit \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --local-repo docs/examples/sync-drift-minimal/local-repo \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

### 4. Revert Unwanted Drift

Use `revert` when the remediation points to:

- `changeOwner=globalBaseline`
- or when local drift is not intentional

Example: revert only one local host file:

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root / \
  --scope local \
  --host-path /etc/kubernetes/kubeadm.yaml
```

Example: revert only one object:

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --kind Secret \
  --namespace default \
  --name grafana-admin-credentials
```

### 5. Re-Run A Read-Only Command

After `commit` or `revert`, run one of:

```bash
sealos sync diff \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

or:

```bash
sealos sync status \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

The expected result is:

- committed local drift disappears because local repo now matches live state
- reverted drift disappears because live state now matches desired state

## What This Example Deliberately Does Not Cover

This example is intentionally minimal. It does not try to show:

- a fully runnable runtime root with real digests, timestamps, and recorded
  state paths
- generated static Pod remediation
- multi-node target resolution
- package rebuild or BOM fork flow

Those are documented elsewhere. This example only exists to pin down the
current single-node `diff -> status -> commit/revert -> diff` operator loop.
