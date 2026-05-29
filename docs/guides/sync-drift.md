# Walkthrough: Inspecting And Reconciling Drift With `sealos sync`

## Status

Current repo walkthrough

## Summary

This walkthrough shows how the current single-node repo handles desired-state
drift with the `sealos sync` command set.

It is intentionally about the implementation that exists today, not a future
controller design. In the current repo, operators can:

- inspect raw drift with `sealos sync diff`
- inspect ownership-grouped drift with `sealos sync status`
- persist supported local drift with `sealos sync commit`
- restore desired state with `sealos sync revert`

The walkthrough focuses on the current boundaries that matter in practice:

- `global` object or host-file drift is treated as `globalBaseline`-owned
- `local` object or host-file drift is treated as local-owned
- generated projections are reported, but not directly committed or reverted

## Related Documents

- Drift tracking and compare model:
  [Materialization and drift](../architecture/materialization-and-drift.md)
- Ownership model:
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- Local repo layout and secret handling:
  [Local repo and secret](../guides/local-repo-and-secret.md)
- Current sync CLI:
  [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go)
- Current compare logic:
  [pkg/distribution/compare/compare.go](../../pkg/distribution/compare/compare.go)
- Current sync command tests:
  [cmd/sealos/cmd/sync_test.go](../../cmd/sealos/cmd/sync_test.go)
- Minimal example directory:
  [docs/examples/sync-drift-minimal](../examples/sync-drift-minimal/README.md)
- Example bundle fixture:
  [docs/examples/sync-drift-minimal/bundle/bundle.yaml](../examples/sync-drift-minimal/bundle/bundle.yaml)
- Example applied revision fixture:
  [docs/examples/sync-drift-minimal/applied-revision.example.yaml](../examples/sync-drift-minimal/applied-revision.example.yaml)
- Example `sync diff` snapshot:
  [docs/examples/sync-drift-minimal/sync-diff.example.yaml](../examples/sync-drift-minimal/sync-diff.example.yaml)
- Example `sync status` snapshot:
  [docs/examples/sync-drift-minimal/sync-status.example.yaml](../examples/sync-drift-minimal/sync-status.example.yaml)

The example directory now also includes both the cluster-local and rendered
policy artifacts:

- `local-repo/policy/local-patch-policy.yaml`
- `bundle/policy/local-patch-policy.yaml`

This matters because current `sync diff` / `sync status` output now exposes the
effective rendered `localPatchPolicy` provenance directly.

## What This Walkthrough Assumes

This walkthrough assumes:

- you already have a rendered bundle directory
- the cluster already has a recorded `AppliedRevision`
- you have a working kubeconfig for that cluster
- if you want to persist local drift, you also have the cluster-local repo
- if you want to inspect host-file drift against something other than `/`, you
  will pass `--host-root`

At a high level, the current command family is:

```bash
sealos sync diff
sealos sync status
sealos sync commit
sealos sync revert
```

### Runtime Root Override

By default, `sync` reads the selected cluster's recorded state, current bundle,
and Clusterfile inventory from the normal sealos runtime root. For tests,
smoke runs, and scripted workflows, pass `--runtime-root` to the concrete sync
subcommand so every state lookup is pinned to the same explicit root:

```bash
sealos sync apply \
  --cluster demo \
  --bundle-dir /tmp/rendered-bundle \
  --runtime-root /tmp/sealos-sync-runtime-root
```

This does not change package semantics. It only controls where `sync` resolves
cluster-local runtime state.

## The Quick Decision Table

Use this table before choosing a command:

| Drift Kind | Typical State | Normal Owner | Normal Next Step |
| --- | --- | --- | --- |
| package-owned Kubernetes object | `Orphan` | `globalBaseline` | revert it, or update the global baseline through package/BOM work |
| local-owned Kubernetes object | `Dirty` | `localOverlay` | commit it if intentional, or revert it |
| package-owned direct host file | `Orphan` | `globalBaseline` | revert it, or update the global baseline |
| local input-backed direct host file | `Dirty` | `localInput` | commit it if intentional, or revert it |
| missing local-owned object or file | `Dirty` | `localOverlay` or `localInput` | revert it; current MVP does not commit a missing projection |
| generated static Pod projection | usually `Orphan` | `localInput`, `globalBaseline`, or `manualReview` | follow the remediation hint; current MVP does not directly commit or revert it |

One practical rule follows:

- `commit` is for supported, intentional, local-owned drift
- `revert` is for bringing live state back to the recorded desired state

## Step 1: Inspect The Raw Drift With `sync diff`

Start with the raw compare view:

```bash
sealos sync diff \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf
```

If you also want host-file drift:

```bash
sealos sync diff \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

What `sync diff` is best at:

- showing the raw `currentCompare` payload
- listing exact mismatch paths such as
  `spec.template.spec.containers[name=cilium-agent].image`
- carrying object-level or host-path-level remediation guidance
- showing whether Sealos persisted an observed summary back into state

In current output, the most important top-level fields are:

- `currentState`
- `localPatchPolicy`
- `currentCompare`
- `observationPersisted`
- `persistedObservedSummary`
- `recordedRevision`

Use `sync diff` when you need the most detailed answer to:

- what drifted
- where it drifted
- who owns that path
- which command is currently safe

If you want a schema-aligned snapshot to compare against, see:

- [docs/examples/sync-drift-minimal/sync-diff.example.yaml](../examples/sync-drift-minimal/sync-diff.example.yaml)

## Step 2: Inspect The Ownership Summary With `sync status`

Then move to the grouped ownership view:

```bash
sealos sync status \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

What `sync status` is best at:

- summarizing `Dirty` vs `Orphan`
- grouping drift into:
  - `dirtyObjects`
  - `orphanObjects`
  - `dirtyHostPaths`
  - `orphanHostPaths`
- showing `mixedOwnershipObjects`
- showing the current live summary side by side with the recorded summary

In current output, the most important top-level fields are:

- `recordedState`
- `recordedObservedSummary`
- `currentState`
- `localPatchPolicy`
- `summary`
- `mixedOwnershipObjects`
- grouped issue lists with remediation blocks

Use `sync status` when you need the most practical answer to:

- is this mostly local drift or `globalBaseline` drift
- which objects are mixed-ownership
- should I be thinking in terms of `commit` or `revert`

If you want a schema-aligned snapshot to compare against, see:

- [docs/examples/sync-drift-minimal/sync-status.example.yaml](../examples/sync-drift-minimal/sync-status.example.yaml)

## Step 3: Read The Remediation Block Before Acting

Current `sync diff` and `sync status` output already includes a remediation
block on supported drifted projections.

The fields that matter most are:

- `action`
- `changeOwner`
- `nextSteps[]`
- `allowedCommands[]`
- `commandGuidance[]`

Today, the ownership routing is:

- `changeOwner=globalBaseline`
  - the drift should be resolved by restoring or updating the selected
    package/BOM global baseline
- `changeOwner=localOverlay`
  - the drift belongs to local repo overlays such as `patches/` or
    `resources/`
- `changeOwner=localInput`
  - the drift belongs to a declared local input binding, often a host-side file
- `changeOwner=manualReview`
  - Sealos cannot safely classify the generated projection automatically

The command guidance is also evaluated, not just listed. The current single-node
MVP uses one especially important precondition:

- `bundleMatchesRecordedDesiredStateDigest`

If this precondition is not satisfied, commands such as `sync commit`,
`sync revert`, and `sync apply` may show up as `blocked`.

Practical operator rule:

- if `sync diff/status` says the command is `blocked`, stop and inspect why the
  bundle is detached from the recorded desired state before changing live state

## Step 4: Commit Intentional Local Drift

Use `sync commit` only for supported local-owned drift:

```bash
sealos sync commit \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --local-repo docs/examples/sync-drift-minimal/local-repo \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

What the current single-node MVP can commit:

- `Dirty` local-owned Kubernetes object drift backed by a tracked
  `localPatch` fragment
- `Dirty` standalone local-owned resource drift backed by
  `local-repo/resources/**`
- `Dirty` local-owned direct host-file drift when the file is backed by a
  declared local input binding

What the current multi-node boundary adds:

- the same local-input-backed host file may appear once per host in
  `sync diff/status`
- if all drifted hosts still have the same live content, `sync commit` can
  safely pick that single value
- if different hosts have different live contents, `sync commit` will refuse
  to guess and require an explicit `--host`
- when `--host` is used and that host has a host-scoped input binding, commit
  writes back to the host-scoped local repo input and its bundle-local
  `components/<component>/host-inputs/<host>/...` copy
- when `--host` is used but the selected host does not have a host-scoped input
  binding, commit still refuses divergent multi-host content instead of
  overwriting the default input with one node's value
- `sync diff/status` also exposes host-scoped input provenance for these paths:
  `dirtyHostPaths` may show `usesHostScopedInput` and `hostInputBindingPath`,
  and `localInputHostSplits` separates hosts that already have scoped payloads
  from hosts that still use the default rendered payload
- `sync diff/status` now also reports this case explicitly as
  `localInputHostSplits`, so operators do not need to infer it from repeated
  dirty host-path rows

Example:

```bash
sealos sync commit \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --local-repo docs/examples/sync-drift-minimal/local-repo \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root / \
  --host 192.168.0.240:22
```

What it does not commit:

- `Orphan` drift
- generated projections
- missing local-owned projections
- symlink-based local host paths
- arbitrary global-baseline changes
- ambiguous multi-node local-input host drift without `--host`

Use `commit` when the right answer is:

- “yes, this local change is intentional”
- “the local repo should now own this updated value”

## Step 5: Revert Unwanted Drift

Use `sync revert` when the right answer is:

- “no, live state should go back to the recorded desired state”

### Revert Everything That Is Currently Tracked

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

### Revert Only Local-Owned Drift

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root / \
  --scope local
```

### Revert One Object

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --kind Secret \
  --namespace default \
  --name grafana-admin-credentials
```

### Revert One Host Path

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root / \
  --host-path /etc/kubernetes/kubeadm.yaml \
  --scope local
```

Current MVP behavior worth remembering:

- missing local-owned objects or files can be restored by `revert`
- generated projections are reported, but are not directly reverted
- local-scope revert will reject clearly global-owned selections

## Step 6: Re-Run `diff` Or `status`

After `commit` or `revert`, run one of the read-only commands again:

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

What you want to see next depends on the action you took:

- after a successful `revert`, the selected drift should disappear from the raw
  compare and summary views
- after a successful `commit`, the same local-owned drift should disappear
  because the local repo now matches the live state

If drift remains, the remaining remediation block is the next source of truth:

- it may point you back to `localOverlay`
- it may point you back to `localInput`
- it may tell you the issue is actually `globalBaseline`

## A Minimal End-To-End Operator Pattern

The shortest safe loop in the current MVP is:

1. Run `sync diff` to inspect the raw mismatch paths.
2. Run `sync status` to see the ownership summary.
3. Follow remediation:
   - `localOverlay` or `localInput` -> `commit` if intentional, otherwise
     `revert`
   - `globalBaseline` -> usually `revert`, unless you are intentionally fixing
     the package or BOM global baseline
4. Re-run `sync diff` or `sync status`.

That loop is already enough to handle the current single-node cases for:

- local Secret or ConfigMap drift
- local patch drift
- direct host-file drift backed by declared local inputs
- `globalBaseline`-owned object or file drift that should be discarded

If you want a concrete sample directory that matches this loop, use:
[docs/examples/sync-drift-minimal](../examples/sync-drift-minimal/README.md).

## Current MVP Limits

This walkthrough only describes the current repo behavior. The current MVP
still does not provide:

- controller-driven multi-node rollout and continuous reconciliation
- direct `commit` or `revert` for generated projections
- arbitrary global-baseline mutation through `commit`
- a full controller-driven background reconcile loop

So the right mental model is:

- the repo already has a working single-node operator loop
- and a narrow CLI-driven multi-node `sync apply` path
- but it is still not a fully autonomous distribution agent
