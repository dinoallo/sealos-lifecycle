# Repository Guidelines

## Project Structure & Module Organization
This repository is a Go workspace (`go.work`) centered on the `sealos` lifecycle toolchain. CLI entrypoints live under `cmd/` (`cmd/sealos`, `cmd/sealctl`, `cmd/lvscare`, `cmd/image-cri-shim`, `cmd/sreg`). Shared implementation lives in `pkg/`. Vendored sibling modules used by the workspace live in `staging/src/github.com/labring/{image-cri-shim,lvscare}`. End-to-end coverage and test helpers live in `test/e2e/`, with fixtures under `test/e2e/testdata/`. Build, release, and image assets live in `scripts/`, `docker/`, and `.github/workflows/`.

## Build, Test, and Development Commands
Use Linux for build and e2e work; `sealos` and `sealctl` require CGO in the default build.

- `make build BINS=sealos`: build one binary into `bin/`.
- `make build`: build all commands under `cmd/`.
- `make format`: run `gofmt` and `goimports`, then format `go.mod`.
- `make lint`: run `golangci-lint` with the repo config.
- `make coverage`: run the race-enabled coverage gate used in CI.
- `go test ./pkg/... ./cmd/...`: fast targeted unit-test pass from the workspace root.
- `cd test/e2e && ginkgo build .`: build the e2e binary used by CI workflows.

## Coding Style & Naming Conventions
Follow idiomatic Go and keep changes localized. Use tabs for indentation, `camelCase` for local identifiers, `PascalCase` for exported names, and `_test.go` for tests. Run `make format` before review. `.golangci.yml` enables strict checks such as `errcheck`, `gosec`, `revive`, `staticcheck`, `ginkgolinter`, and `forbidigo`, so avoid `print/println`, unchecked errors, and weak test names. Keep package names short and lowercase.

## Testing Guidelines
Prefer focused unit tests next to the changed package, for example `go test ./pkg/apply/...`. Use table-driven tests where existing files do. E2E coverage uses Ginkgo v2 in `test/e2e`; name specs by behavior and filter locally with `ginkgo --focus="E2E_sealos_run_test"`. Regenerate bundled e2e fixtures with `cd test/e2e && go-bindata -nometadata -pkg testdata -ignore=testdata.go -o testdata/testdata.go testdata/` when testdata changes.

## Commit & Pull Request Guidelines
This checkout has no local git history, so follow the repository-wide default: Conventional Commits such as `fix(apply): handle empty host list`. Keep commits scoped to one change. PRs should summarize behavior changes, list validation commands, link related issues, and attach logs or screenshots when CLI output or workflow behavior changes. Note Linux-only or environment-specific verification explicitly.
