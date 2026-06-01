# Kind: DistributionHealthProof

## 状态

已实现的文件 schema。

## 类别

证据文档。

## 维护方

验证系统、release automation 或 release owner 写入 health proof 文档。人工 reviewer 可以基于它批准 promotion。

## 常见位置

- `proofs/<distribution>/<revision>/<channel>.yaml`
- `evidence/health/<distribution>/<revision>.yaml`

## 用途

`DistributionHealthProof` 记录目标 revision 是否通过 promotion 所需的健康信号。它会附加到 `ReleaseChannel` 的 promotion history 中，应被视为 evidence，而不是源侧意图。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionHealthProof
metadata:
  name: sealos-v5.0.0-stable
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `line` | 是 | Distribution line。后续 schema 扩展时，新 writer 也可以镜像为 `distribution`。 |
| `targetRevision` | 是 | 正在评估的 revision。 |
| `passed` | 是 | 整体 pass/fail 结果。 |
| `summary` | 否 | 面向人的健康摘要。 |
| `collectedAt` | 是 | 证据采集时间，RFC3339 格式。 |
| `signals` | 否 | 单个健康信号。 |

## Signal 契约

每个 signal 记录：

- `name`
- `passed`
- `message`

Signal 应具备确定性，并且可追溯到日志、测试输出或 `PackageAcceptanceReport`。

## 校验规则

- 必须设置 `apiVersion`、`kind` 和 `metadata.name`。
- 必须设置 `targetRevision`。
- `passed` 必须反映 required signals 的聚合结果。
- `collectedAt` 必须是 RFC3339 时间戳。
- Health proof 文档不能包含 secret 值。

## 生命周期

1. Test 或 acceptance workflow 评估 candidate revision。
2. Workflow 写入 health proof 文档。
3. Release promotion 在 `ReleaseChannel.promotionHistory` 中引用该 proof。
4. Reviewer 和 automation 使用该 proof 审计 revision 为什么被推进。

## 边界

- `DistributionHealthProof` 本身不选择被 promotion 的 revision。
- `DistributionHealthProof` 不替代 package acceptance report。
- `DistributionHealthProof` 不携带 kubeconfig、token 或 secret data。
- `DistributionHealthProof` 发布后不应被修改。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionHealthProof
metadata:
  name: sealos-v5.0.0-stable
spec:
  line: sealos
  targetRevision: v5.0.0
  passed: true
  summary: all required package acceptance checks passed
  collectedAt: "2026-06-01T00:00:00Z"
  signals:
    - name: package-acceptance
      passed: true
      message: acceptance report completed successfully
    - name: revert-check
      passed: true
      message: no managed object drift after revert
```

## 相关 Kind

- `PackageAcceptanceReport` 可以提供原始 acceptance details。
- `ReleaseChannel` 在 promotion 时引用 health proof。
- `BOM` 标识正在测试的 revision。
