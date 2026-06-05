# Controller/Fleet 可观测性 Runbook

## 状态

当前运维契约

## 目的

这份 runbook 定义当前 `sealos-agent --controller` fleet 的可观测面。它把
`DistributionTarget` status、`DistributionRolloutPolicy`、Kubernetes events、
controller logs 和 smoke artifacts 转成统一的 target 聚合、rollout 进度、健康证据、
promotion gate 和失败归档口径。

当前 controller 还没有为这些字段提供专用 metrics endpoint，也没有在 CRD 中持久化
per-host rollout cursor。请把 Kubernetes API objects 和下面捕获的 artifacts 当成事实源。
外部 dashboard 可以 watch `sealos-system` namespace 下的 `DistributionTarget` 和
`DistributionRolloutPolicy`，采集同一组字段。

## Fleet 状态聚合

每次 fleet 检查先从 CRD printer columns 开始：

```bash
kubectl -n sealos-system get distributiontargets
kubectl -n sealos-system get distributionrolloutpolicies
```

构建 dashboard 行或分页摘要时，读取结构化 status：

```bash
kubectl -n sealos-system get distributiontarget <target> -o yaml
kubectl -n sealos-system describe distributiontarget <target>
kubectl -n sealos-system get events \
  --field-selector involvedObject.kind=DistributionTarget,involvedObject.name=<target> \
  --sort-by=.lastTimestamp
```

每个 target 行应展示：

| 列 | 来源 |
| --- | --- |
| Namespace | `metadata.namespace` |
| Target | `metadata.name` |
| Cluster | `spec.clusterName` 或 `status.lastResult.clusterName` |
| Generation | `metadata.generation` |
| Observed generation | `status.observedGeneration` |
| Phase | `status.phase` |
| Ready | `status.conditions[type=Ready]` |
| Degraded | `status.conditions[type=Degraded]` |
| Revision | `status.lastResult.revision` |
| Channel | `status.lastResult.channel` |
| Desired digest | `status.lastResult.desiredStateDigest` |
| Applied revision | `status.lastResult.appliedRevisionPath` |
| Last reconcile | `status.lastReconcileTime` |
| Retry count | `status.retryCount` |
| Next retry | `status.nextRetryTime` |
| Hold reason | `status.holdReason` |
| Last diagnostic | `status.lastDiagnostic.reason` 和 `message` |

fleet 层使用这些聚合计数：

| 计数 | 规则 |
| --- | --- |
| `targetsTotal` | 所有 `DistributionTarget` 对象数量 |
| `targetsReady` | `Ready=True` 且 `phase=Succeeded` |
| `targetsDegraded` | `Degraded=True` |
| `targetsPaused` | `phase=Paused` |
| `targetsRollbackHold` | `phase=RollbackHold` |
| `targetsRetrying` | `phase=Retrying` |
| `targetsPartiallyFailed` | `phase=PartiallyFailed` |
| `targetsStaleGeneration` | `status.observedGeneration < metadata.generation` |
| `targetsDigestMismatch` | 期望统一时，同一 line/channel 下出现不同 `desiredStateDigest` |

## Rollout 进度

当前 rollout 进度来自 target phase、target events、被引用的
`DistributionRolloutPolicy` 和 controller logs。executor 以 rendered bundle 作为 rollout
单元；符合条件的 host-targeted steps 可以按 policy 分批、canary、canary 后暂停、health gate
和失败 rollback。CRD 里还没有持久 per-host cursor。

针对每个 target，先捕获 rollout policy 和 event stream：

```bash
kubectl -n sealos-system get distributiontarget <target> \
  -o jsonpath='{.spec.rolloutPolicyRef.name}{"\n"}'
kubectl -n sealos-system get distributionrolloutpolicy <policy> -o yaml
kubectl -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent --since=1h
```

phase 解释：

| Phase | 含义 | Operator 动作 |
| --- | --- | --- |
| `Succeeded` | 最新 reconcile 完成，且 `Ready=True`。 | 保留证据用于 promotion gate。 |
| `Retrying` | reconcile 失败，`retryBackoff` 已安排下一次尝试。 | 在下次 retry 前检查 `lastDiagnostic`、events 和 logs。 |
| `PartiallyFailed` | agent 同时返回了结果和错误。 | 归档 partial result，重试前检查已改变的状态。 |
| `Paused` | canary 后暂停，且不是 degraded。 | 评审 canary 健康证据，然后更新 target 或 policy 继续。 |
| `RollbackHold` | 已触发回滚到上一次成功 rendered revision。 | 作为 hold 处理；选择下一 revision 前归档失败证据。 |

## 健康证据

target status 证明 controller 最后选择并应用了什么，但它不替代详细的 health proof 文档。

单个 target 的最小健康证据：

- reconcile 后的 target YAML
- `status.lastResult.bomName`、`revision`、`channel` 和
  `desiredStateDigest`
- `status.lastResult.bundlePath`
- `status.lastResult.appliedRevisionPath`
- `Ready` 和 `Degraded` conditions
- 最近的 `DistributionTarget` events
- reconcile 时间窗口内的 controller logs
- 如果该 reconcile 来自测试，保留 smoke 或 acceptance artifact directory

当 candidate 用于 promotion 时，还要附上匹配的 `DistributionHealthProof`，以及生成该 proof
的 package 或 PoC acceptance report。health proof 是 promotion 证据；controller target
status 是 runtime 证据，证明 candidate 实际在哪些目标上 reconciled。

## Promotion Gate

controller-backed promotion gate 在推进 release channel 前，应要求全部检查通过：

| Gate | 必需证据 |
| --- | --- |
| Target selection | `DistributionTarget.spec` 选择了预期 BOM、本地 channel 或 release metadata line/channel。 |
| Reconciled generation | `status.observedGeneration == metadata.generation`。 |
| Ready state | `phase=Succeeded`、`Ready=True` 且 `Degraded=False`。 |
| Revision identity | `status.lastResult.revision` 和 `desiredStateDigest` 匹配 candidate BOM。 |
| Health proof | candidate revision 和目标 channel 有一份通过的 `DistributionHealthProof`。 |
| Artifact retention | bundle path、applied revision path、controller logs 和 events 已归档。 |
| Hold clearance | cohort 中没有 `Paused`、`RollbackHold`、`Retrying` 或 `PartiallyFailed` target，除非 promotion 明确排除它。 |

controller 本身不会推进 channel。promotion 仍然要通过 release metadata service 或
`sealos sync promote`，并携带 health proof evidence。

## 失败归档

每个 `Degraded=True`、`Retrying`、`PartiallyFailed` 或 `RollbackHold` target 都应创建
incident directory，并保存：

```bash
mkdir -p /tmp/sealos-controller-incident/<target>
kubectl -n sealos-system get distributiontarget <target> -o yaml \
  > /tmp/sealos-controller-incident/<target>/target.yaml
kubectl -n sealos-system describe distributiontarget <target> \
  > /tmp/sealos-controller-incident/<target>/target.describe.txt
kubectl -n sealos-system get events \
  --field-selector involvedObject.kind=DistributionTarget,involvedObject.name=<target> \
  --sort-by=.lastTimestamp \
  > /tmp/sealos-controller-incident/<target>/target.events.txt
kubectl -n sealos-system get pods -l app.kubernetes.io/name=sealos-distribution-controller -o yaml \
  > /tmp/sealos-controller-incident/<target>/controller-pods.yaml
kubectl -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent --since=2h \
  > /tmp/sealos-controller-incident/<target>/controller.log
```

如果可用，也复制引用的 bundle directory、applied revision file、health proof、acceptance
report 和真实集群 smoke diagnostics。不要归档 kubeconfig 内容、Secret object payload、
private key 或带 token 的文件。path、digest、object name、events 和 normalized status
fields 才是预期诊断面。

## 告警路由

使用这些告警标题：

| 条件 | 标题 |
| --- | --- |
| `Degraded=True` | `distribution target degraded: <namespace>/<target> <reason>` |
| `phase=Retrying` 且超过 `nextRetryTime` | `distribution target retry overdue: <namespace>/<target>` |
| `phase=PartiallyFailed` | `distribution target partially failed: <namespace>/<target>` |
| `phase=Paused` | `distribution rollout paused after canary: <namespace>/<target>` |
| `phase=RollbackHold` | `distribution rollback hold: <namespace>/<target>` |
| stale generation | `distribution target generation not observed: <namespace>/<target>` |

package-owned baseline 失败路由给 release owner；local repo 和 patch 失败路由给 cluster
owner；host tool preflight 失败路由给生命周期节点 owner；rollback hold 同时路由给 release
owner 和 cluster owner。

## Closeout

incident 或 promotion observation 只有在满足以下条件后才能关闭：

1. `kubectl get distributiontarget <target> -o yaml` 显示预期 phase 和 conditions。
2. `status.lastResult` 中包含预期 revision 和 desired digest。
3. reconcile 时间窗口内的 events 和 logs 已归档。
4. 该证据用于 promotion 时，已经链接匹配的 health proof 或 acceptance report。
5. 如果 incident 涉及 drift 或 rollback，已经附上变更后的 `sync status` 或 controller
   target status snapshot。
