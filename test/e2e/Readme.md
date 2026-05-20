## Run the Tests

You can run focused e2e specs through `go test` and pass Ginkgo filters via `-args`.
If you want the standalone CLI, install Ginkgo v2:

```console
go install github.com/onsi/ginkgo/v2/ginkgo@latest
```

```shell
ginkgo --help
  --focus value
        If set, ginkgo will only run specs that match this regular expression.
```

### Run image-cri-shim Tests

Test the image-cri-shim component that brings up the cluster through `sealos`.

```shell
sealos run labring/kubernetes:v1.25.0
ginkgo -v --focus="image-cri-shim test" ./test/e2e
```

### Run the Sync Drift Smoke Test

The sync drift smoke test validates the single-node `sealos sync` loop with:

- the docs fixture under `docs/examples/sync-drift-minimal/`
- the fake `kubectl` helper under `test/e2e/testhelper/fakekubectl/`
- both `sync commit` and `sync revert` flows

It does not require a real kube-apiserver. The spec is skipped by default and only runs when
`SEALOS_E2E_SYNC_SMOKE=true` is set.

```shell
cd $PROJECT_ROOT
make build BINS=sealos
go test ./test/e2e/testhelper/fakekubectl

cd test/e2e
SEALOS_E2E_SYNC_SMOKE=true \
SEALOS_E2E_TEST_SEALOS_BIN_PATH=$PROJECT_ROOT/bin/linux_amd64/sealos \
go test . -run TestSealosTest -count=1 -args --ginkgo.focus=E2E_sealos_sync_drift_smoke_test
```

GitHub Actions also exposes the same smoke path through
`.github/workflows/e2e_sync_drift_smoke.yml`.
It runs automatically for pull requests and `main` pushes that touch the sync/distribution/e2e fixture paths,
and it can still be launched manually with `workflow_dispatch`.
The reusable workflow at `.github/workflows/e2e_sync_drift_smoke_reusable.yml` also accepts
`go_version`, `timeout_minutes`, and `upload_debug_artifacts` inputs for other callers.
When `upload_debug_artifacts=true`, failed GitHub Actions runs keep the smoke temp directories
and upload them as an artifact for inspection. Each preserved smoke directory includes
`commands.log`, `commands.yaml`, `metadata.yaml`, and `failure-summary.yaml` so you can reconstruct
the paths used by the failed run and quickly identify the last failing `sealos sync` invocation.
The reusable workflow also writes a GitHub job summary: on failure it points at the uploaded
artifact and, when preserved artifacts are enabled, inlines the recorded `failure-summary.yaml`.
`metadata.yaml` also records optional CI fields such as `githubRunId` and `githubSha`; local runs
typically leave those values empty. It now also carries an `inputs` section that captures the
effective smoke toggles and any workflow-supplied values such as `go_version`,
`timeout_minutes`, and `upload_debug_artifacts`.
Even when the temp directory is not preserved, the smoke test prints the same failure summary to
the Ginkgo output on failure.

### Testdata

Use `go-bindata` to package testdata:

https://github.com/go-bindata/go-bindata/tree/v3.1.1

```shell
cd test/e2e && go-bindata -nometadata -pkg testdata -ignore=testdata.go -o testdata/testdata.go testdata/
```
