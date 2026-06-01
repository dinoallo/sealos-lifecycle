# Kind: AppliedInventory

## 状态

提议中的文件 schema。

## 类别

运行时 inventory 文档。

## 维护方

Apply 或 inventory workflow 写入该文档。

## 常见位置

- `state/<cluster>/applied-inventory.yaml`
- `out/state/<cluster>/applied-inventory.yaml`

## 用途

`AppliedInventory` 在 `AppliedRevision` 基础上展开具体的 Kubernetes objects、host paths、local resources 和 package-owned resources，描述 apply 后预期存在的资源。它为 drift detection 和 orphan cleanup 提供更强基础。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedInventory
metadata:
  name: prod-01-v5.0.0
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `clusterName` | 是 | 集群名称。 |
| `revision` | 是 | 已应用 distribution revision。 |
| `desiredStateDigest` | 是 | 与 `AppliedRevision` 匹配的 desired state digest。 |
| `bundleDigest` | 否 | 生成 inventory 所用 hydrated bundle 的 digest。 |
| `k8sObjects` | 否 | 受管 Kubernetes object identities。 |
| `hostPaths` | 否 | 受管 host path identities 和 ownership metadata。 |
| `localResources` | 否 | Apply 创建或消费的本地资源。 |
| `components` | 否 | 按 component 分组的 inventory。 |

## Inventory Identity

Inventory entry 应包含稳定 identifier：

- Kubernetes group、version、kind、namespace 和 name；
- host path 以及 target node 或 target selector；
- component 和 package ownership；
- 可用时记录 hash 或 digest。

## 校验规则

- 必须设置 `clusterName`、`revision` 和 `desiredStateDigest`。
- Inventory identities 必须归一化。
- 重复的 managed object identity 无效。
- Entry 应尽可能标识 owning component 或 package。
- 不能嵌入 secret data。

## 生命周期

1. Hydration 预测 managed resources。
2. Apply 确认创建、更新或保留了什么。
3. Inventory workflow 写入 applied inventory。
4. Drift detection 对比 observed state 和 inventory。
5. Cleanup 用 inventory 区分 managed orphan 和 unmanaged resource。

## 边界

- `AppliedInventory` 是生成的 runtime inventory，不是 source intent。
- `AppliedInventory` 不替代 `AppliedRevision`。
- `AppliedInventory` 不应包含 secret contents。
- `AppliedInventory` 不应被手工编辑。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedInventory
metadata:
  name: prod-01-v5.0.0
spec:
  clusterName: prod-01
  revision: v5.0.0
  desiredStateDigest: sha256:...
  k8sObjects:
    - group: ""
      version: v1
      kind: ConfigMap
      namespace: kube-system
      name: kubeadm-config
      component: kubernetes
  hostPaths:
    - path: /etc/kubernetes/manifests/kube-apiserver.yaml
      target: allMasters
      component: kubernetes
```

## 相关 Kind

- `AppliedRevision` 记录紧凑 apply state。
- `HydratedBundle` 预测 desired inventory。
- `DistributionTarget` status 可以链接到 generated inventory。
