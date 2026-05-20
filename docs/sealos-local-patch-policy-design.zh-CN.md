# 子设计：Local Patch Policy 的来源与 Scope

## 状态

基于当前单节点 MVP 行为的草案

## 摘要

这份文档定义 `LocalPatchPolicy` 到底允许治理什么、它允许来自哪里，以及
Sealos 在 render 之后应该如何携带它的 provenance。

当前决策刻意收得很窄：

- `LocalPatchPolicy` 只治理 cluster-local override surface
- policy 文档的 scope 必须是 `clusterLocal`
- 当前只支持两种来源：
  - `localRepo`
  - `builtInDefault`
- 当前 MVP 里，package 和 BOM 都不负责定义 local-patch policy
- render 结束后，bundle 中携带的 policy artifact 会成为 compare、validation
  和 `sync commit` 共同消费的有效 policy source of truth

## 相关文档

- local repo 布局与 Secret 处理：
  [sealos-local-repo-and-secret-guide.md](./sealos-local-repo-and-secret-guide.md)
- Local patch policy 的编写与评审流程：
  [sealos-local-patch-policy-authoring-and-review.md](./sealos-local-patch-policy-authoring-and-review.md)
- 追踪与 drift 模型：
  [sealos-materialization-tracking-and-drift-detection-model.md](./sealos-materialization-tracking-and-drift-detection-model.md)
- ownership 与 reconcile 模型：
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- operator action 速查：
  [sealos-sync-operator-action-reference.md](./sealos-sync-operator-action-reference.md)
- 当前 policy schema：
  [pkg/distribution/ownership/document.go](../pkg/distribution/ownership/document.go)
- 当前 rendered-policy 处理路径：
  [pkg/distribution/hydrate/policy.go](../pkg/distribution/hydrate/policy.go)
- 当前 plan 组装路径：
  [pkg/distribution/reconcile/materialize.go](../pkg/distribution/reconcile/materialize.go)

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

### 2. 当前只支持 `localRepo` 和 `builtInDefault`

当前 bundle provenance 模型只支持两种 policy source：

- `localRepo`
  - cluster-local repo 明确提供了
    `policy/local-patch-policy.yaml`
- `builtInDefault`
  - local repo 没有提供 policy，于是 Sealos 把内置默认 policy 渲染进了
    bundle

这层 provenance 会记录在 bundle metadata 里：

- `bundle.spec.localPatchPolicySource`
- `bundle.spec.localPatchPolicyScope`
- `bundle.spec.localPatchPolicyName`
- `bundle.spec.localPatchPolicyPath`
- `bundle.spec.localPatchPolicyDigest`

### 3. Package 和 BOM 当前都不负责定义 Local-Patch Policy

当前 MVP 刻意不把 package-side 或 BOM-side local-patch policy 当成合法来源。

这不是实现没做完，而是当前架构下的明确选择：

- package/BOM content 定义 shared baseline
- `LocalPatchPolicy` 定义哪些 cluster-local mutation 是被允许的
- 如果让 shared baseline producer 悄悄扩大可本地修改的 surface，就会把
  global/local ownership boundary 搅混

换句话说：

- package/BOM 可以定义 extension point
- 但当前它们不定义 cluster 的 local mutation policy

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
2. 否则，用内置默认 policy 文档。
3. 把选中的文档渲染到 `bundle/policy/local-patch-policy.yaml`。
4. 在 `bundle.yaml` 里记录 source、scope、name、path、digest。
5. 后续 consumer 一律读 rendered bundle artifact，而不是回头读环境里的
   local repo。

当前 MVP 不做多层 policy merge。

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

### Package-Scoped Local Patch Policy

当前不采纳。因为这会让 package producer 为所有消费集群定义本地 mutation
envelope，这比当前 ownership 模型允许的权力更大。

### BOM-Scoped Local Patch Policy

当前不采纳。因为 BOM 仍然属于 global release selection 的一部分。它可以选择
baseline artifact，但当前不拥有 cluster-local override budget。

### Package + BOM + Local Repo 多层 Merge

当前不采纳。因为在更简单的“单一 policy 文档”模型还没被完全验证之前，这会过早
引入 precedence 复杂度。

## 未来扩展门槛

如果 Sealos 以后真的需要 package/BOM-scoped policy，那也不应该作为“悄悄加上
第三种 source”来做。

它应该走一份单独设计，至少回答这些问题：

- 新 scope 的语义到底是什么
- 谁来 review “允许本地修改的 surface 被扩大” 这件事
- cluster-local policy 是否只能进一步收紧，而不能扩大 shared policy
- rendered provenance 应该如何区分 baseline-owned policy 和 cluster-local
  policy

在那份设计出现之前，当前规则保持简单：

- policy scope 是 `clusterLocal`
- 支持的 source 只有 `localRepo` 和 `builtInDefault`
