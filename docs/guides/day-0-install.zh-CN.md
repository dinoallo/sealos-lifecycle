# Day 0 安装流程

## 状态

当前实现指南，并记录产品化方向。

这份文档说明在 Sealos Distribution 体系里，operator 如何从没有集群走到安装好
一个 Sealos 集群。当前仓库已经验证的是最小单节点 prepared-host 路径。
multi-node Day 0 bootstrap、registry/API-backed release lookup、完整产品化
release assets 仍在 hardening。

## 心智模型

Day 0 安装不是让用户手工跑每个 package。正确路径是先选择 release target，
把这个 target render 成 cluster-local desired-state bundle，然后 apply。

这条路径里的对象是：

- `ComponentPackage`：一个可安装组件包，通常发布为带 digest pin 的 OCI image。
- `BOM`：一个不可变 release snapshot，选择精确的组件 package artifact。
- `ReleaseChannel`：一个可变 release 指针，把 distribution line 和 channel
  解析到某个 BOM revision。当前实现支持本地 `ReleaseChannel` 文件。
- `LocalRepo`：集群本地输入、资源、patch 和 policy。
- rendered bundle：`sealos sync render` 生成的 desired state。
- `AppliedRevision`：记录已 render 或已 apply target 的集群本地状态。

## 产品目标流程

对普通安装用户来说，package payload 应该已经由 release build 发布好。用户只
消费 digest-pinned BOM，或者消费能解析到该 BOM 的 `ReleaseChannel`。

最终产品形态里，用户不应该运行
`scripts/poc/minimal-single-node/stage-assets.sh`。这个脚本属于 PoC 的
release-build 侧，因为它负责把 package template 填成带 runtime、Kubernetes
和 Cilium 真实 payload 的 package。

目标流程是：

1. 选择 `distribution line + channel`，或选择一个 explicit BOM revision
2. 如果 BOM 需要本地输入，初始化并填写 cluster-local repo
3. 校验 source inputs
4. render bundle
5. 执行 apply preflight
6. apply
7. 检查 status 和集群健康

## 前置条件

当前单节点安装路径要求 host 是 prepared Linux host 或 VM：

- `systemd` 是 PID 1
- `sync apply` 可以用 root 权限执行
- swap 已关闭
- kernel 能支持当前 CNI/runtime profile
- 已安装必要 host commands，包括 `systemctl`、`modprobe`、`sysctl`、
  `conntrack`、`crictl`、`socat` 和 `curl`
- 选中的 BOM 或本地 `ReleaseChannel` 指向真实 package payload，而不是
  placeholder package template

从本仓库 checkout 开发时先构建 `sealos`：

```bash
make build BINS=sealos
SEALOS="$(pwd)/bin/linux_amd64/sealos"
```

如果使用 release binary：

```bash
SEALOS="$(command -v sealos)"
```

render、apply、status 和 drift 命令应使用同一个 runtime root。这样可以避免
普通用户 `${HOME}/.sealos` 和 root `${HOME}/.sealos` 在 host bootstrap 时分裂
成两套状态。

```bash
RUNTIME_ROOT=/var/lib/sealos/runtime
sudo install -d -m 0755 "$RUNTIME_ROOT"
```

## 选择目标

如果集群要固定到一个明确 revision，使用 explicit BOM：

```bash
CLUSTER=poc-minimal
TARGET_BOM=/var/lib/sealos/distribution/releases/default-platform/rev-007/bom.yaml
LOCAL_REPO=/var/lib/sealos/distribution/${CLUSTER}/local-repo
KUBECONFIG=/etc/kubernetes/admin.conf
BUNDLE="${RUNTIME_ROOT}/${CLUSTER}/distribution/bundles/current"
```

如果集群要跟随 channel，使用本地 `ReleaseChannel` 文件：

```bash
CLUSTER=poc-minimal
TARGET_CHANNEL=/var/lib/sealos/distribution/channels/default-platform/stable.yaml
LOCAL_REPO=/var/lib/sealos/distribution/${CLUSTER}/local-repo
KUBECONFIG=/etc/kubernetes/admin.conf
BUNDLE="${RUNTIME_ROOT}/${CLUSTER}/distribution/bundles/current"
```

下面命令里使用 `--file "$TARGET_BOM"` 或
`--release-channel "$TARGET_CHANNEL"` 二选一，不能同时传。

## 用 explicit BOM 安装

初始化 local repo skeleton：

```bash
sudo $SEALOS sync local-repo init \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --file "$TARGET_BOM" \
  --output-dir "$LOCAL_REPO"
```

填写 local repo 里生成的占位内容。这些是被 package 要求的集群本地 inputs 和
resources。

检查 local repo：

```bash
sudo $SEALOS sync local-repo doctor \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --file "$TARGET_BOM" \
  --local-repo "$LOCAL_REPO"
```

校验完整 source side：

```bash
sudo $SEALOS sync validate \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --file "$TARGET_BOM" \
  --local-repo "$LOCAL_REPO"
```

render desired-state bundle：

```bash
sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --file "$TARGET_BOM" \
  --local-repo "$LOCAL_REPO"
```

执行 apply preflight：

```bash
sudo $SEALOS sync preflight \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"
```

apply bundle：

```bash
sudo $SEALOS sync apply \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"
```

检查安装结果：

```bash
kubectl --kubeconfig "$KUBECONFIG" get nodes
kubectl --kubeconfig "$KUBECONFIG" get pods -A
sudo $SEALOS sync status \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER"
```

## 用 ReleaseChannel 安装

使用同一条流程，但在 `local-repo init`、`local-repo doctor`、`validate`
和 `render` 里把 `--file "$TARGET_BOM"` 替换为
`--release-channel "$TARGET_CHANNEL"`：

```bash
sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-channel "$TARGET_CHANNEL" \
  --local-repo "$LOCAL_REPO"
```

当前 resolver 只读取本地 `ReleaseChannel` 文件，还没有实现按 distribution line
和 channel 做 registry/API-backed lookup。

## 当前 PoC 快捷入口

仓库里的最小单节点 PoC 可以直接使用 wrapper：

```bash
sudo scripts/poc/minimal-single-node/bootstrap.sh \
  --cluster poc-minimal
```

这个 wrapper 会：

1. 构建 `sealos`
2. 启动临时本地 registry
3. 发布三个 PoC package 为 OCI package image
4. 从生成的 OCI-backed BOM render
5. 执行 `sealos sync apply`
6. 执行 PoC validator

这个入口适合仓库开发和验证，不是长期面向普通用户的安装入口。

## 仅开发使用的本地 package 流程

开发者在本仓库里迭代 package 目录时，可以用 `--package-source` 绕过已发布的
OCI package：

```bash
$SEALOS sync render \
  --cluster poc-minimal \
  --file scripts/poc/minimal-single-node/bom.yaml \
  --package-source containerd=scripts/poc/minimal-single-node/packages/containerd \
  --package-source kubernetes=scripts/poc/minimal-single-node/packages/kubernetes \
  --package-source cilium=scripts/poc/minimal-single-node/packages/cilium
```

这条开发路径要求先把 package template 填上真实 assets，所以 PoC 里有：

```bash
scripts/poc/minimal-single-node/stage-assets.sh \
  --kubelet-bin /usr/bin/kubelet \
  --cilium-manifest /absolute/path/to/cilium.yaml
```

这应该被视为 release builder 或 developer 的工作。普通安装用户应该消费已经
发布并 digest-pinned 的 package。

## 完成标准

Day 0 完成的标准是：

- `sync apply` 成功
- `/etc/kubernetes/admin.conf` 存在
- `kubectl get nodes` 显示预期 node 集合为 `Ready`
- cluster critical workloads，包括选定 CNI，处于健康状态
- `sealos sync status --cluster <name>` 报告预期 BOM revision 和 desired state

## 当前边界

- 本地 `ReleaseChannel` resolver 仍是 file-backed。
- 最小 PoC 仍偏 single-node prepared-host。
- `stage-assets.sh` 存在，是因为本仓库不会直接提交大型 runtime/Kubernetes/Cilium
  payload 作为 release artifacts。
- 产品化安装应把 package assembly 移到 release build/publish 自动化里，让用户
  只需要选择 target 并执行 validate/render/apply。
