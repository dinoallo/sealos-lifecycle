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

如果集群要从 release metadata source 按 channel 解析目标：

```bash
CLUSTER=poc-minimal
RELEASE_SOURCE=https://release.sealos.example
RELEASE_LINE=default-platform
RELEASE_CHANNEL=stable
LOCAL_REPO=/var/lib/sealos/distribution/${CLUSTER}/local-repo
KUBECONFIG=/etc/kubernetes/admin.conf
BUNDLE="${RUNTIME_ROOT}/${CLUSTER}/distribution/bundles/current"
```

下面命令里必须只选择一种 target selector：

- `--file "$TARGET_BOM"`
- `--release-channel "$TARGET_CHANNEL"`
- `--release-source "$RELEASE_SOURCE" --release-line "$RELEASE_LINE" --channel "$RELEASE_CHANNEL"`

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
`--release-channel "$TARGET_CHANNEL"`。如果使用 release service lookup，把 target
flags 替换为
`--release-source "$RELEASE_SOURCE" --release-line "$RELEASE_LINE" --channel "$RELEASE_CHANNEL"`：

```bash
sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-channel "$TARGET_CHANNEL" \
  --local-repo "$LOCAL_REPO"
```

```bash
sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"
```

release metadata source 必须在 `/v1/distributions/{line}/channels/{channel}`
返回 `ReleaseChannel` 文档。解析出的 channel 必须指向带 `spec.bomDigest`
的 BOM；Sealos 会在 render 前校验获取到的 BOM digest。

## 不依赖脚本的最小 PoC

仓库里的 PoC 现在应该按上面的 0-to-1 operator flow 执行。PoC 安装路径不再调
`fetch-assets.sh`、`stage-assets.sh`、`publish-oci.sh`、`render.sh` 或
`bootstrap.sh` 这类 helper。

这些 helper 仍可能存在于仓库中，用于 CI fixture 生成和 release-build 实验；
原因是本仓库不会直接提交大型 runtime、Kubernetes 或 Cilium payload。它们不是
面向 operator 的 PoC 入口。

先选择一个已经发布好 component packages、并且 BOM 已经 digest-pinned 的 release
target：

```bash
CLUSTER=poc-minimal
RUNTIME_ROOT=/var/lib/sealos/runtime
RELEASE_SOURCE=/var/lib/sealos/distribution/release-source
RELEASE_LINE=minimal-single-node
RELEASE_CHANNEL=alpha
LOCAL_REPO=/var/lib/sealos/distribution/${CLUSTER}/local-repo
KUBECONFIG=/etc/kubernetes/admin.conf
BUNDLE="${RUNTIME_ROOT}/${CLUSTER}/distribution/bundles/current"
```

如果使用本地文件系统 release source，先确认 channel metadata 存在：

```bash
test -f "${RELEASE_SOURCE}/channels/${RELEASE_LINE}/${RELEASE_CHANNEL}.yaml"
```

从这个 target 初始化 cluster-local repo：

```bash
sudo $SEALOS sync local-repo init \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --output-dir "$LOCAL_REPO" \
  --overwrite
```

对于当前仓库里的 PoC 默认输入，可以从已跟踪的 package 默认文件填充生成的
inputs：

```bash
sudo install -D -m 0644 \
  scripts/poc/minimal-single-node/packages/containerd/files/etc/containerd/config.toml \
  "${LOCAL_REPO}/inputs/containerd/containerd-config.toml"
sudo install -D -m 0644 \
  scripts/poc/minimal-single-node/packages/kubernetes/files/etc/kubernetes/kubeadm.yaml \
  "${LOCAL_REPO}/inputs/kubernetes/kubeadm-cluster-config.yaml"
sudo install -D -m 0644 \
  scripts/poc/minimal-single-node/packages/cilium/files/values/basic.yaml \
  "${LOCAL_REPO}/inputs/cilium/cilium-values.yaml"
```

然后直接执行 guide 里的命令链路：

```bash
sudo $SEALOS sync local-repo doctor \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"

sudo $SEALOS sync validate \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"

sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"

sudo $SEALOS sync preflight \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"

sudo $SEALOS sync apply \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"
```

用普通集群命令和 distribution 命令验收：

```bash
kubectl --kubeconfig "$KUBECONFIG" get nodes -o wide
kubectl --kubeconfig "$KUBECONFIG" -n kube-system rollout status ds/cilium --timeout=180s
kubectl --kubeconfig "$KUBECONFIG" -n kube-system rollout status deploy/cilium-operator --timeout=180s
kubectl --kubeconfig "$KUBECONFIG" -n kube-system rollout status deploy/coredns --timeout=180s
sudo $SEALOS sync status \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER"
```

multi-node PoC 使用同一组命令，但 cluster name 需要已经有对应的 Sealos
`Clusterfile` 和 SSH inventory。`sync render`、`sync plan`、`sync preflight`
和 `sync apply` 会从 cluster state 里解析 `allNodes`、`firstMaster` 和
cluster-scoped targets；不需要 PoC wrapper。

安全的 multi-node Day 0 acceptance gate 可以这样运行：

```bash
make verify-day0-multinode-acceptance
```

这条 gate 会用三节点 inventory render PoC package set，在 `sync plan` 中检查
`allNodes`、`firstMaster` 和 `cluster` target resolution，并通过 fake-remote
reconcile 覆盖 kubeadm join config 生成、远端 first-master kubeconfig 拉取和
multi-node execution targeting。它也保留 Cilium package 在 render set 里，让
application/CNI package 成为 Day 0 acceptance 的一部分。GitHub workflow
`.github/workflows/day0_multi_node_acceptance.yml` 会运行同一个不修改 host 的安全
gate。

这条 gate 是仓库验证入口，不会修改 host。

如果要对不依赖脚本的 guide 路径做不修改 host 的仓库检查，可以提供一个已存在的
release source：

```bash
make verify-day0-guide-render \
  DAY0_RELEASE_SOURCE=/var/lib/sealos/distribution/release-source \
  DAY0_RELEASE_LINE=minimal-single-node \
  DAY0_RELEASE_CHANNEL=alpha \
  DAY0_CLUSTER=poc-minimal-ci
```

这个 target 会执行 `local-repo init`，填入当前 PoC 默认 local inputs，运行
`local-repo doctor`、`validate` 和 `render`，然后检查 bundle 与
applied-revision 文件已经写出。它不会 fetch assets、发布 OCI packages，也不会
apply 到 host。

## Package Set 边界

当前 Day 0 PoC release set 故意只覆盖可安装的 cluster baseline：

| Package | Owner | Required Local Input | Health Check |
| --- | --- | --- | --- |
| `containerd-runtime` | node runtime platform owner | `containerd-config` | runtime service 和本地 runtime tooling 健康 |
| `kubernetes-rootfs` | cluster platform owner | `kubeadm-cluster-config` | kube-apiserver 可达、node 已注册、bootstrap manifests 已 apply |
| `cilium-cni` | network platform owner | `cilium-values` | Cilium DaemonSet 和 operator rollout 完成 |

下一批 package set 扩展是产品契约，还不是当前 PoC BOM 的一部分：

- `kubernetes-control-plane-patch`：SRE 维护的 hardening overlays，带
  policy/admission/static-pod inputs，以及 API/static-Pod projection healthcheck。
- `csi-driver-*`：storage owner 维护的 addon，带 backend Secret refs、
  topology/storage-class inputs、controller/node healthchecks 和 data-plane
  protection notes。
- `ingress-controller-*`：network/edge owner 维护的 addon，带 ingress class、
  exposure/TLS/load-balancer inputs，以及 route/webhook healthchecks。
- `observability-stack`：observability owner 维护的 addon，带 retention、
  storage、external endpoint inputs，以及 collector/dashboard/alert healthchecks。

这些 package 只有在 package directory、local repo templates、healthcheck hook、
acceptance evidence 和 rollback/reset 边界都齐备后，才能进入 Day 0。

## 可重复运行的清理流程

不依赖脚本的 PoC 有一个清理入口，用来清除可以重新生成的状态：rendered bundle
state、cluster-local repo、临时 workdir，以及可选的远端 staged bundle mirror。
默认清理路径不会删除 Kubernetes、CRI、kubelet、containerd、`Clusterfile`、
`admin.conf` 或 host 数据：

```bash
make cleanup-day0-poc \
  DAY0_CLEANUP_ARGS="--cluster poc-minimal \
    --runtime-root /var/lib/sealos/runtime \
    --distribution-root /var/lib/sealos/distribution"
```

如果 multi-node rerun 中 `sync apply` 已经把 staged bundle mirror 复制到了远端
hosts，并且该 cluster 在默认 runtime root 下有 `sealos exec -c <cluster>` 可用的
`Clusterfile`，可以显式加 `--remote-staged`：

```bash
make cleanup-day0-poc \
  DAY0_CLEANUP_ARGS="--cluster sealos-distribution-test --remote-staged"
```

重置 Kubernetes/CRI 状态是单独的破坏性操作，永远不是默认 cleanup 的一部分。
只有在一次性 PoC 主机上才使用：

```bash
I_UNDERSTAND_THIS_MUTATES_HOST=1 make reset-day0-poc \
  DAY0_CLEANUP_ARGS="--cluster poc-minimal"
```

对于使用 `--runtime-root /var/lib/sealos/runtime` 的不依赖脚本安装，rerender 之前
优先用安全 cleanup target。只有当目标 cluster 同时存在于 `sealos reset` 使用的
默认 Sealos runtime root 中时，才使用 `reset-day0-poc`，因为 `sealos reset` 不支持
`--runtime-root`。

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

这是 renderer 开发路径，不是 0-to-1 PoC 安装路径。只有这些本地 package 目录
已经包含完整真实 payload 时，它才具备可安装性。普通安装用户应该通过 BOM 或
`ReleaseChannel` 消费已经发布并 digest-pinned 的 packages。

产品形态不提供 package-direct install 路径。`sealos sync package pull` 或
`--package-source` 这类能力是 package authoring 和 renderer 开发工具，不替代
Day 0 release contract：operator 仍然必须选择 BOM 或 `ReleaseChannel`，render
bundle，执行 preflight，然后 apply 这个 bundle。把安装执行收敛在 BOM/bundle
边界之后，才能保留依赖排序、本地输入绑定、render provenance、drift ownership
和 rollback history。

## 完成标准

Day 0 完成的标准是：

- `sync apply` 成功
- `/etc/kubernetes/admin.conf` 存在
- `kubectl get nodes` 显示预期 node 集合为 `Ready`
- cluster critical workloads，包括选定 CNI，处于健康状态
- `sealos sync status --cluster <name>` 报告预期 BOM revision 和 desired state

## 当前边界

- 面向 operator 的 PoC 假设 release assets 已经存在。package assembly 属于
  release build/publish 自动化。
- helper scripts 仍保留在仓库里，用于 CI fixture 生成和 package 开发，但它们
  不再是 PoC 安装入口。
- multi-node Day 0 已有 CLI-driven render/plan/preflight/apply 支持；默认 GitHub
  gate 仍是不修改 host 的安全 gate，除非使用受保护环境跑真实主机。
