# 数据面保护 Runbook

## 状态

当前运维契约

## 目的

这份 runbook 定义 operator 执行 `sync revert`、controller rollback、upgrade 或 derived
distribution line 切换时，如何保护 Secret、PVC、数据库资源和运行时生成状态。

规则很直接：desired-state reconcile 可以修配置，但不能静默替换、删除或 promotion
数据面状态。除非 package 明确声明了经过评审的数据迁移流程，否则 Secret 字节、
PersistentVolumeClaim 数据、数据库内容、生成的连接 Secret 和 backup artifact 都应视为
cluster-local runtime state。

## 数据面对象

任何 mutating command 前，都先用这张 ownership map 分类：

| 对象 | 示例 | 默认 Ownership | 允许的自动化 |
| --- | --- | --- | --- |
| Secret references | values 文件里的 Secret name、`secretRef`、TLS Secret name | local input | render/apply 可以引用名字 |
| Local Secret manifests | `local-repo/resources/secrets/*.yaml` | local overlay | owner approval 后可以 apply 和 scoped revert |
| External secret references | `ExternalSecret`、`SecretProviderClass`、store refs | local overlay | apply 引用，不复制 provider payload |
| Runtime-generated Secrets | KubeBlocks account Secrets、operator 创建的 connection Secrets | runtime-local | 只观察 |
| PVCs and PVs | `PersistentVolumeClaim`、保留的 `PersistentVolume` | data-plane runtime | 观察并防止删除 |
| Database resources | KubeBlocks `Cluster`、PostgreSQL data、backup schedules | package baseline plus runtime data | 只有通过 backup gate 后才 apply spec change |
| Backup artifacts | snapshots、dump files、object-store backup ids | data-plane evidence | 归档引用，不归档凭据 |

## 变更前 Preflight

在 `sync revert`、`sync apply`、controller rollback 或 derived line switch 触碰 stateful
component 前，执行这份 checklist：

1. 用 `sync status` 和 `sync diff` 找出受影响对象。
2. 把每个对象分类为 configuration、local Secret material、PVC、database control-plane
   object 或 runtime-generated state。
3. 对 BOM 和 local repo 跑 `sealos sync validate`，让 Secret manifest 权限和 local-resource
   规则先被检查。
4. 捕获当前 Kubernetes 状态：

   ```bash
   kubectl get secret,pvc,pv -A -o yaml
   kubectl get cluster.apps.kubeblocks.io -A -o yaml
   kubectl get events -A --sort-by=.lastTimestamp
   ```

5. 确认每个受影响 database 或 PVC owner 都有近期 backup 或 snapshot。只记录 backup id、
   timestamp、namespace、object name 和不含凭据的 storage location。
6. 当 mutation 跨 ownership boundary 时，确认 package owner 和 cluster owner 都同意。

如果会把 Secret payload、kubeconfig、private key 或 provider token 复制进 incident
artifact，就不要继续。

## 操作 Gate

| 操作 | 必需 Gate | 何时阻断 |
| --- | --- | --- |
| 对 Secret 执行 `sync revert` | local Secret manifest 或 external reference 的 owner approval。 | live Secret 是 operator 生成的，或包含不在 local repo 里的 rotated credentials。 |
| 对 PVC 或 database objects 执行 `sync revert` | backup evidence 加 package owner approval。 | revert 会删除 PVC、修改 retention policy，或降级 storage/database version。 |
| Controller rollback | 存在 last successful revision，且 stateful components 有 backup evidence。 | rollback 会改变 database topology、storage class、Secret references 或 PVC templates，但没有 migration note。 |
| Upgrade | package acceptance evidence 加 data migration 或 compatibility notes。 | 缺少必需 backup、healthcheck hooks 没覆盖 database，或 upgrade 改了不可逆 storage settings。 |
| Derived line switch | package replacements diff 加 runtime-state compatibility check。 | derived BOM 修改 database ownership、Secret names、PVC names 或 storage policy，但没有 transfer plan。 |

## Secrets

Secret 名字和引用可以属于 rendered desired state。Secret 字节仍然属于 local。

允许：

- apply 有意存放在 `local-repo/resources/` 下的 local Secret manifest
- apply `ExternalSecret` 或 `SecretProviderClass` 引用
- Secret owner 确认 recorded value 仍然有效后，把 local Secret object revert 回 local repo
  manifest

阻断：

- 把运行时轮换过的 Secret 字节 commit 回 package artifact
- 归档 `data`、`stringData`、kubeconfig 内容、provider token 或 private key
- 把 operator 生成的 connection Secret 当成 package-owned desired state

验证：

```bash
sealos sync status --cluster <cluster> --bundle-dir <bundle> --kubeconfig <kubeconfig> --host-root /
kubectl -n <namespace> get secret <name> -o jsonpath='{.metadata.name}{"\n"}'
```

只验证 identity、type、labels、annotations、owner references 和预期 consumer health。不要打印
Secret payload。

## PVCs And Databases

PVC、PV 和数据库内容是数据面状态。package 可以拥有数据库 control-plane manifest，但 live data
不是 package baseline。

修改 database package、PVC template、storage class、retention policy、topology 或 service
version 前：

1. 捕获 database custom resource 和 PVC 列表。
2. 确认 backup freshness 和 restore scope。
3. 确认 package release notes 或 version change migration notes。
4. 运行 package healthcheck hooks 或 application acceptance workflow。
5. 记录 previous/target BOM revision、desired digest 和 package digests。

变更后：

1. 确认 database custom resource 按它的 operator 语义 Ready。
2. 确认 PVC 仍然存在，并且绑定到预期 storage class。
3. 确认 application healthcheck hooks 通过。
4. 确认 `sync status` 不再报告 stateful component 的非预期 drift。
5. 把 backup id、restore instructions 和 post-change health evidence 附到工单。

## Revert And Rollback

只有在完成对象分类后，才用 `sync revert` 处理 configuration drift。只有 data-plane
compatibility gate 通过后，才用 controller rollback 恢复 last successful rendered revision。

不要把 broad revert 或 rollback 当成 database restore 机制。如果 data 已损坏，先走数据库 owner
的 restore procedure，等 runtime state 稳定后再 reconcile 配置。

rollback hold 场景：

- 归档 failed target YAML、controller logs、events、bundle path 和 applied revision path
- 保留 last successful bundle 和 current failed bundle digest
- 记录 rollback 是否触碰 Secret references、PVC templates、storage classes、database
  versions 或 backup schedules
- 选择下一 revision 前要求明确 owner approval

## Upgrades And Derived Line Switches

把 upgrade 和 derived line switch 当成 package identity change 加 runtime compatibility check。

切换前：

- 比较 source 和 target BOM revisions
- 列出 changed component packages 和 digests
- 识别 stateful package changes
- 验证 Secret reference names 保持兼容，或已经有 planned rotation
- 验证 PVC names、storage classes 和 retention policies 保持兼容
- 验证 database service versions 和 migration requirements

切换后：

- 捕获 target status 或 `sync status`
- 运行 application 和 database healthchecks
- 确认 agreed restore window 内旧 backup 仍可使用
- 归档 derived BOM labels，说明 source line 和 revision

如果 derived line 有意改变 stateful component ownership，写一份 handoff note，包含 old owner、
new owner、protected objects、backup evidence、restore owner 和 validation command。

## 应保留的证据

每个 data-plane-sensitive 工单都应保留：

- `sync status -o yaml` 和 `sync diff -o yaml`
- mutation 后的 target 或 controller status
- 受影响对象 identity list，不包含 Secret payload
- previous/target BOM revision 和 desired-state digests
- changed stateful components 的 package digests
- backup 或 snapshot id 和 timestamp
- database 或 application healthcheck output
- Secret、PVC、database 或 derived-line changes 的 owner approvals
- rollback 或 restore instructions

这些证据应证明该操作只改变了预期配置，没有丢失或 promotion 运行时数据面状态。
