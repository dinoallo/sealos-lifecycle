# Kind: HydratedBundle

## 状态

已实现的生成文件 schema。

## 类别

生成的 bundle 文档。

## 维护方

Hydration workflow 写入该文档。人可以 review，但不应手工编辑。

## 常见位置

- `out/hydrated/<cluster>/<revision>/bundle.yaml`
- `bundles/<cluster>/<revision>/hydrated-bundle.yaml`

## 用途

`HydratedBundle` 记录目标集群和 revision 的完整渲染后 desired state。它捕获 source provenance、local patch policy provenance、local resources、Kubernetes objects、host paths 和 component render output。

它是声明式源文档与 runtime apply 之间的桥梁。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: HydratedBundle
metadata:
  name: prod-01-v5.0.0
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `bomName` | 是 | Hydration 使用的 BOM 名称。 |
| `revision` | 是 | BOM 或 release revision。 |
| `channel` | 否 | 解析 revision 时使用的 release channel。 |
| `renderProvenance` | 是 | Source documents、digests、local repo revision 和 package sources。 |
| `sourcePreflight` | 否 | Render 前的 source validation summary。 |
| `executionTopology` | 否 | Cluster 或 host targets 的 execution plan 形态。 |
| `localPatchPolicySource` | 否 | Local patch policy 来源。 |
| `localPatchPolicyScope` | 否 | Local patch policy 覆盖范围。 |
| `localPatchPolicyName` | 否 | Policy 名称。 |
| `localPatchPolicyPath` | 否 | Policy 路径。 |
| `localPatchPolicyDigest` | 否 | Policy digest。 |
| `localResources` | 否 | Hydration 创建或消费的本地资源。 |
| `trackedK8sObjects` | 否 | 该 bundle 管理的 Kubernetes objects。 |
| `trackedHostPaths` | 否 | 该 bundle 管理的 host paths。 |
| `components` | 是 | 渲染后的 component outputs。 |

## Provenance 要求

`renderProvenance` 应包含：

- 使用 channel 时的 `distributionChannelPath` 和 digest；
- `distributionLine`；
- `bomPath` 和 digest；
- 使用本地模式时的 `localRepoPath` 和 local revision；
- `localPatchRevision`；
- package source paths 和 digests。

这些 provenance 必须足以复现或审计渲染后的 desired state。

## 校验规则

- 必须设置 `apiVersion`、`kind` 和 `metadata.name`。
- 必须设置 `bomName` 和 `revision`。
- Digest 如果存在，必须使用支持的 digest 格式。
- Tracked object identity 必须稳定。
- Host path 必须归一化且不能有歧义。
- Bundle 不能嵌入 secret material。

## 生命周期

1. 解析 source documents 和 cluster intent。
2. 运行 source preflight 和 local patch gates。
3. Hydration 将 package output 渲染成 bundle。
4. Apply 消费该 bundle。
5. Runtime state 通过 `AppliedRevision` 记录已应用 bundle。

## 边界

- `HydratedBundle` 是生成输出，不是 source intent。
- `HydratedBundle` 不应被手工编辑。
- `HydratedBundle` 不批准 local patch gates。
- `HydratedBundle` 不是长期运行时 status 对象。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: HydratedBundle
metadata:
  name: prod-01-v5.0.0
spec:
  bomName: sealos-v5.0.0
  revision: v5.0.0
  channel: stable
  renderProvenance:
    distributionChannelPath: channels/sealos/stable.yaml
    distributionChannelDigest: sha256:...
    bomPath: boms/sealos/v5.0.0/bom.yaml
    bomDigest: sha256:...
    localRepoPath: /var/lib/sealos/distribution/local-repo
    localRepoRevision: 2026-06-01T00-00-00Z
    packageSources:
      - component: kubernetes
        path: packages/core/kubernetes/v1.31.1
        digest: sha256:...
  localPatchPolicyPath: ownership/local-patch-policy.yaml
  localPatchPolicyDigest: sha256:...
  components:
    - name: kubernetes
      version: v1.31.1
```

## 相关 Kind

- `BOM` 提供被选择的 packages。
- `ClusterTarget` 和 `ComponentInput` 提供集群特定意图。
- `LocalPatchPolicy` 控制 local patch 边界。
- `AppliedRevision` 记录应用结果。
- `AppliedInventory` 未来可扩展 managed object inventory。
