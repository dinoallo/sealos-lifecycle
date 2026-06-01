# Kind: LocalPatchPolicyGateApproval

## 状态

已实现的文件 schema。

## 类别

Approval evidence 文档。

## 维护方

批准 owner 写入或签署该文档。Automation 会校验 approval 是否匹配检测到的 gate violation。

## 常见位置

- `approvals/local-patch-policy/<approval>.yaml`
- `clusters/<cluster>/approvals/<approval>.yaml`

## 用途

`LocalPatchPolicyGateApproval` 记录对 local patch policy gate violation 的显式批准，例如扩大 policy scope 或接受不兼容的 local patches。

它是某个具体风险已被 review 的 evidence，不是通用 bypass。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicyGateApproval
metadata:
  name: kubernetes-policy-widening-2026-06-01
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `owner` | 是 | 负责 approval 的 owner。 |
| `approvedBy` | 是 | 批准变更的人、团队或 automation identity。 |
| `changeRef` | 是 | 被批准变更的稳定引用。 |
| `expiresAt` | 否 | RFC3339 过期时间。 |
| `oldPolicy` | 否 | 旧 policy 的引用或 digest。 |
| `newPolicy` | 是 | 新 policy 的引用或 digest。 |
| `approvals` | 是 | 针对具体 gate violation 的 approval entries。 |

每个 approval entry 应包含：

- violation code
- expected count
- expected impact
- reason

## Gate Codes

已知 gate code 包括：

- `wideningChange`
- `incompatiblePatches`
- `approvalExpiresSoon`

## 校验规则

- Approval 必须匹配正在评估的 policy references。
- Approval 不能过期。
- Expected impact 必须匹配检测到的 gate impact。
- Approval 不能批准超过声明数量的 violations。
- 对需要 gate 的 violation，如果缺少 approval，校验必须失败。

## 生命周期

1. Policy validation 检测到 gate violation。
2. 责任 owner review 该变更。
3. Owner 为精确变更写入 gate approval。
4. Automation 在 hydration 或 apply 时校验 approval。
5. Evidence 被保留用于审计。

## 边界

- 该文档不定义 local patch policy。
- 该文档不包含 patch 内容。
- 该文档不批准无关的未来变更。
- 该文档不能包含 secret 值。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicyGateApproval
metadata:
  name: kubernetes-policy-widening-2026-06-01
spec:
  owner: kubernetes-package-owner
  approvedBy: release-team
  changeRef: git:abc123
  expiresAt: "2026-07-01T00:00:00Z"
  oldPolicy:
    path: ownership/local-patch-policy.yaml
    digest: sha256:...
  newPolicy:
    path: ownership/local-patch-policy.yaml
    digest: sha256:...
  approvals:
    - code: wideningChange
      expectedCount: 1
      expectedImpact: allow rootfs etc/kubernetes patches
      reason: production cluster requires kubeadm config override
```

## 相关 Kind

- `LocalPatchPolicy` 定义被批准的 policy。
- `HydratedBundle` 记录 policy 和 approval provenance。
- `AppliedRevision` 可以暴露 policy 相关 conditions。
