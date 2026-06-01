# Kind: LocalPatchPolicy

## 状态

已实现的文件 schema。

## 类别

Ownership 和 policy 文档。

## 维护方

Package owner 和本地 cluster owner 共同维护该策略。策略应让 ownership 边界在 patch 应用前可 review。

## 常见位置

- `ownership/local-patch-policy.yaml`
- `packages/<category>/<name>/ownership/local-patch-policy.yaml`
- `clusters/<cluster>/ownership/local-patch-policy.yaml`

## 用途

`LocalPatchPolicy` 声明本地 patch 可以修改什么、由谁拥有这些变更，以及哪些变更需要显式 approval。它让集群可以保留本地差异，同时不丢失上游 package ownership 的控制。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: kubernetes-local-patches
spec: {}
```

## Spec 形态

策略包含 scope 和 policy rules。具体 rule 字段可以演进，但文档必须回答这些问题：

- 该策略覆盖哪个 package、component 或 cluster scope？
- 哪些文件、Kubernetes objects 或 host paths 可以被本地 patch？
- 哪些变更总是允许？
- 哪些变更需要 gate approval？
- 哪些变更被禁止？
- 每个 policy area 由谁 review？

## 校验规则

- 必须设置 `apiVersion`、`kind` 和 `metadata.name`。
- Scope 必须显式。
- Path 和 selector 必须具备确定性。
- Policy 不能授予未声明 package 文件的 ownership。
- 扩大 policy 范围应触发 gate approval。
- 不兼容 local patches 应触发 gate violation。

## 生命周期

1. Package 或 cluster owner 定义允许的本地 patch 边界。
2. Hydration 或 apply 前根据 policy 检查 local patches。
3. Gate violation 需要 `LocalPatchPolicyGateApproval`。
4. Hydration 在 `HydratedBundle` 中记录 policy source 和 digest。
5. Runtime drift report 区分预期本地变更和 unmanaged drift。

## 边界

- `LocalPatchPolicy` 不携带 patch 内容本身。
- `LocalPatchPolicy` 本身不批准 policy widening。
- `LocalPatchPolicy` 不包含 secrets。
- `LocalPatchPolicy` 不替代 runtime drift evidence。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: kubernetes-local-patches
spec:
  scope:
    component: kubernetes
    package: core/kubernetes
  policy:
    allow:
      - path: patches/kubernetes/manifests/**
        owner: cluster-platform
    requireApproval:
      - path: rootfs/etc/kubernetes/**
        owner: kubernetes-package-owner
    deny:
      - path: rootfs/usr/bin/kubelet
        reason: binary replacement must come from source build
```

## 相关 Kind

- `ComponentPackage` 可以引用 local patch policy。
- `BOM` 可以引用 release-level local patch policy。
- `LocalPatchPolicyGateApproval` 批准 gate violations。
- `HydratedBundle` 记录渲染时使用的 policy。
