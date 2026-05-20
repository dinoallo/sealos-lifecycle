## 运行测试

你可以直接用 `go test` 跑聚焦过的 e2e spec，并通过 `-args` 传递 Ginkgo 过滤条件。
如果你想单独安装 Ginkgo CLI，请使用 v2：

```console
go install github.com/onsi/ginkgo/v2/ginkgo@latest
```

```shell
ginkgo --help
  --focus value
        If set, ginkgo will only run specs that match this regular expression.
```

### 运行 image-cri-shim 测试

这个测试会通过 `sealos` 拉起集群并验证 image-cri-shim。

```shell
sealos run labring/kubernetes:v1.25.0
ginkgo -v --focus="image-cri-shim test" ./test/e2e
```

### 运行 Sync Drift Smoke Test

这条 smoke 会验证单节点 `sealos sync` 闭环，覆盖：

- `docs/examples/sync-drift-minimal/` 里的文档 fixture
- `test/e2e/testhelper/fakekubectl/` 里的 fake `kubectl`
- `sync commit` 和 `sync revert` 两条路径

它不依赖真实 kube-apiserver。这个 spec 默认会被跳过，只有显式设置
`SEALOS_E2E_SYNC_SMOKE=true` 才会执行。

```shell
cd $PROJECT_ROOT
make build BINS=sealos
go test ./test/e2e/testhelper/fakekubectl

cd test/e2e
SEALOS_E2E_SYNC_SMOKE=true \
SEALOS_E2E_TEST_SEALOS_BIN_PATH=$PROJECT_ROOT/bin/linux_amd64/sealos \
go test . -run TestSealosTest -count=1 -args --ginkgo.focus=E2E_sealos_sync_drift_smoke_test
```

GitHub Actions 里也暴露了同一条 smoke 入口：
`.github/workflows/e2e_sync_drift_smoke.yml`。
当和 `sync/distribution/e2e fixture` 相关的路径发生变更时，它会在 Pull Request 和推送到 `main` 时自动运行；
同时也保留 `workflow_dispatch` 的手动触发方式。
另外，可复用 workflow `.github/workflows/e2e_sync_drift_smoke_reusable.yml`
还支持给其他调用方传 `go_version`、`timeout_minutes` 和 `upload_debug_artifacts` 三个输入。
当 `upload_debug_artifacts=true` 时，GitHub Actions 失败运行会保留 smoke 的临时目录，
并把它们作为 artifact 上传，方便排查。每个保留下来的 smoke 目录里都会带上
`commands.log`、`commands.yaml`、`metadata.yaml` 和 `failure-summary.yaml`，
方便回放这次失败运行使用到的关键路径，并快速定位最后一次失败的 `sealos sync` 调用。
这个可复用 workflow 还会写一份 GitHub job summary：失败时会指向上传的 artifact，
而且在开启保留 artifact 的情况下，会直接内嵌记录下来的 `failure-summary.yaml`。
`metadata.yaml` 里还会记录可选的 CI 字段，比如 `githubRunId`、`githubSha`；
本地运行时这些值通常会保持为空。现在它还会带一个 `inputs` 小节，把这次 smoke
实际生效的开关和 workflow 传入的 `go_version`、`timeout_minutes`、
`upload_debug_artifacts` 等值一起记录下来。
即使没有保留临时目录，smoke 在失败时也会把同一份 failure summary 直接打印到 Ginkgo 输出里。

### Testdata

使用 `go-bindata` 打包 testdata：

https://github.com/go-bindata/go-bindata/tree/v3.1.1

```shell
cd test/e2e && go-bindata -nometadata -pkg testdata -ignore=testdata.go -o testdata/testdata.go testdata/
```
