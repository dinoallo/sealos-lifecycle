# Kind: ComponentInput

## 状态

提议中的文件 schema。

## 类别

集群意图文档。

## 维护方

Cluster 配置 owner 维护 `ComponentInput` 文档。引入新 input 时，通常需要 package owner 参与 review。

## 常见位置

- `clusters/<cluster>/inputs/<component>.yaml`
- `inputs/<cluster>/<component>.yaml`

## 用途

`ComponentInput` 为 `ComponentPackage` 声明的 inputs 提供集群特定的非 secret 值。它把 package defaults 和集群特定配置分开。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentInput
metadata:
  name: prod-01-kubernetes
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `component` | 是 | 接收这些值的 component。 |
| `values` | 是 | 按已声明 input contract 填写的结构化非 secret 值。 |
| `profile` | 否 | 当一个文件服务多个 profile 时使用的 profile selector。 |
| `targetRevision` | 否 | 当 values 明确与某个 revision 绑定时可选 pin。 |

## 校验规则

- `component` 必须匹配被选择的 `ComponentPackage`。
- 每个顶层 value 应对应一个已声明的 package input。
- Values 不能包含 secrets。
- 当 value 是路径时，除非 input contract 明确允许 runtime path，否则必须是仓库相对路径。
- 未知 value 默认应校验失败，除非 package 明确允许开放结构输入。

## 生命周期

1. Package 声明可接受的 inputs。
2. Cluster owner 通过 `ComponentInput` 提供集群特定值。
3. Hydration 合并 package defaults 和 component inputs。
4. `HydratedBundle` 记录 input source provenance，但不记录 secret material。

## 边界

- `ComponentInput` 不定义 package contents。
- `ComponentInput` 不选择 distribution revision。
- `ComponentInput` 不能携带 secret 值。
- `ComponentInput` 不表示运行时状态。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentInput
metadata:
  name: prod-01-kubernetes
spec:
  component: kubernetes
  values:
    clusterCIDR: 10.244.0.0/16
    serviceCIDR: 10.96.0.0/12
    controlPlaneEndpoint: api.prod-01.example.com:6443
```

## 相关 Kind

- `ComponentPackage` 声明接受的 inputs。
- `ClusterTarget` 引用 component input 文件。
- `HydratedBundle` 记录渲染值和 input provenance。
