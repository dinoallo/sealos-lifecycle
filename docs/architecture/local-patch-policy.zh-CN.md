# 子设计：Local Patch Policy 的来源与 Scope

## 状态

已实现的 MVP 契约

## 摘要

这份文档定义 `LocalPatchPolicy` 到底允许治理什么、它允许来自哪里，以及
Sealos 在 render 之后应该如何携带它的 provenance。

当前决策刻意收得很窄：

- `LocalPatchPolicy` 只治理 cluster-local override surface
- policy 文档的 scope 必须是 `clusterLocal`
- 当前支持这些来源：
  - `localRepo`
  - `bom`
  - `package`
  - `builtInDefault`
- package 和 BOM 可以选择一份 cluster-local policy artifact，但不会因此创建
  package/BOM-scoped policy surface
- render 结束后，bundle 中携带的 policy artifact 会成为 compare、validation
  和 `sync commit` 共同消费的有效 policy source of truth
- canonical resolver 是 `hydrate.SelectLocalPatchPolicy`；render 和
  `sync validate` 使用同一份 source-selection 与 precedence 结果

## 相关文档

- local repo 布局与 Secret 处理：
  [Local repo and secret](../guides/local-repo-and-secret.md)
- Local patch policy 的编写与评审流程：
  [Local patch policy authoring](../guides/local-patch-policy-authoring.md)
- 追踪与 drift 模型：
  [Materialization and drift](../architecture/materialization-and-drift.md)
- ownership 与 reconcile 模型：
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- operator action 速查：
  [Sync operator actions](../reference/sync-operator-actions.md)
- 当前 policy schema：
  [pkg/distribution/ownership/document.go](../../pkg/distribution/ownership/document.go)
- 当前 rendered-policy 处理路径：
  [pkg/distribution/hydrate/policy.go](../../pkg/distribution/hydrate/policy.go)
- 当前 plan 组装路径：
  [pkg/distribution/reconcile/materialize.go](../../pkg/distribution/reconcile/materialize.go)
- operator preflight 输出：
  [cmd/sealos/cmd/sync_validate.go](../../cmd/sealos/cmd/sync_validate.go)

## 为什么需要单独一份设计

package-format 设计已经定义了：

- package content 里可以有什么
- 哪些 input 可以从包外绑定
- 哪些 content 默认属于 `global`

local-repo guide 也已经定义了：

- cluster-local 的值和资源应该放在哪里
- 为什么它们不该悄悄改写 shared package artifact

真正还模糊的是更窄的一层：

- local-patch allowlist 本身到底谁有权定义
- 这份 allowlist 到底属于 package/BOM baseline，还是 cluster-local state
- rendered bundle 应该如何证明自己到底用了哪份 policy

这份文档就是把这层缺口补上。

## 决策

### 1. Policy Scope 永远是 `clusterLocal`

`LocalPatchPolicy` 不是在描述可复用的 package 行为，而是在描述“一个集群被允许
使用哪些本地 override surface”。

所以 policy 对象本身就应该属于 cluster-local ownership：

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
```

在当前 MVP 里：

- `spec.scope: clusterLocal` 是唯一支持的值
- 对 legacy policy 文档，scope 缺失时会按 `clusterLocal` 解释
- 任何其他 scope 都会被拒绝

### 2. 支持的 Policy Sources

当前 bundle provenance 模型支持这些 policy source：

- `localRepo`
  - cluster-local repo 明确提供了
    `policy/local-patch-policy.yaml`
- `bom`
  - 被选中的 BOM 通过 `spec.localPatchPolicy` 引用了一份经过评审的
    policy 文件
- `package`
  - 正好一个被 BOM 选中的 component package 通过 `spec.localPatchPolicy`
    引用了一份经过评审的 policy 文件
- `builtInDefault`
  - 没有任何显式 source 提供 policy，于是 Sealos 把内置默认 policy 渲染进了
    bundle

这层 provenance 会记录在 bundle metadata 里：

- `bundle.spec.localPatchPolicySource`
- `bundle.spec.localPatchPolicyScope`
- `bundle.spec.localPatchPolicyName`
- `bundle.spec.localPatchPolicyPath`
- `bundle.spec.localPatchPolicyDigest`

### 3. Package 和 BOM Source 仍然携带 `clusterLocal` Policy

package-side 和 BOM-side source 可以为 rendered bundle 选择生效的
cluster-local policy。它们不会引入 package/BOM-scoped policy。

- package/BOM content 定义 shared baseline
- `LocalPatchPolicy` 定义哪些 cluster-local mutation 是被允许的
- policy 文档本身仍然必须使用 `spec.scope: clusterLocal`

换句话说：

- package/BOM 可以定义 extension point
- package/BOM 可以选择一份经过评审的 policy artifact
- package/BOM 当前不能定义不同的 ownership scope，也不能合并 policy layers

### 4. Render 后的 Bundle 才是有效 Policy Carrier

一旦 render 完成，effective policy 就不应该再从环境里的 local repo 状态临时推断。
它应该成为 bundle revision 自己携带的一部分。

之后所有 consumer 都应该读 bundle 里的这份 policy，用来做：

- render 阶段的 local patch validation
- compare 阶段的 `policyEligible` 标注
- `sync commit` 阶段的 local patch overlay 提取
- `sync diff` / `sync status` 里的 operator 可见 provenance 输出

这样 rendered revision 才是自描述的。

## 解析规则

当前解析顺序是：

1. 如果 `local-repo/policy/local-patch-policy.yaml` 存在，就用它。
2. 否则，如果被选中的 BOM 声明了 `spec.localPatchPolicy`，就按 BOM 文件所在
   目录解析并加载这份 policy。
3. 否则，如果正好一个被选中的 component package 声明了
   `spec.localPatchPolicy`，就按该 package root 解析并加载这份 policy。
4. 否则，用内置默认 policy 文档。
5. 把选中的文档渲染到 `bundle/policy/local-patch-policy.yaml`。
6. 在 `bundle.yaml` 里记录 source、scope、name、path、digest。
7. 后续 consumer 一律读 rendered bundle artifact，而不是回头读环境里的
   local repo。

步骤 1-4 的 canonical implementation 是
`hydrate.SelectLocalPatchPolicy`。`sync render` 会用这个选择结果来 materialize
bundle，`sync validate` 也会通过 `localPolicySource`、`localPolicy`、
`localPolicyName`、`localPolicyScope` 和 `localPolicyCandidates` 暴露同一个
决策。candidates 列表是给 operator 审计用的视图，用来说明有哪些外部 policy
声明存在，以及最终是哪一个赢得 precedence。

当前 MVP 不做多层 policy merge。如果 package source 会成为生效 source，并且
多于一个 package 声明了 policy，render 会直接失败，而不是猜测。

每个 rendered bundle revision 只有一份有效 policy 文档。

## Legacy 兼容

当前故意保留两条兼容规则：

- legacy bundle 如果没有显式记录 local-patch policy metadata，仍然按
  built-in default policy 解释
- legacy policy 文档如果没有 `spec.scope`，仍然按 `clusterLocal` 解释

兼容就到这里为止。

如果 bundle 显式声明了不支持的 provenance，或者它记录下来的
name/scope/path/digest 和 rendered artifact 对不上，policy consumer
应该直接拒绝这个 bundle，而不是猜测。

## 当前不采纳的方案

### Package-Scoped Policy Scope

当前不采纳。package 可以选择一份 `clusterLocal` policy artifact，但它不定义
package-owned policy scope。

### BOM-Scoped Policy Scope

当前不采纳。BOM 可以为 rendered revision 选择一份 `clusterLocal` policy
artifact，但它不定义 BOM-owned policy scope。

### Package + BOM + Local Repo 多层 Merge

当前不采纳。因为在更简单的“单一 policy 文档”模型还没被完全验证之前，这会过早
引入 precedence 复杂度。

## 未来扩展门槛

如果 Sealos 以后真的需要 package/BOM-scoped policy，那也不应该复用当前这些
source-selection 字段来悄悄实现。

它应该走一份单独设计，至少回答这些问题：

- 新 scope 的语义到底是什么
- 谁来 review “允许本地修改的 surface 被扩大” 这件事
- cluster-local policy 是否只能进一步收紧，而不能扩大 shared policy
- rendered provenance 应该如何区分 baseline-owned policy 和 cluster-local
  policy

在那份设计出现之前，当前规则保持简单：

- policy scope 是 `clusterLocal`
- 支持的 source 是 `localRepo`、`bom`、`package` 和 `builtInDefault`
- 每个 rendered bundle 只有一份生效 policy
