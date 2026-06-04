# Walkthrough: Packaging Cilium As A Sealos Component

## Status

Current repo walkthrough

## Summary

This walkthrough shows how the repository currently packages Cilium for the
package-based distribution flow.

It describes the implementation that exists today in the minimal single-node
PoC:

- Cilium is packaged as an `application` component package
- the package payload is manifest-based, not rootfs-based
- the package is built into an OCI image with `sealos sync package build`
- the package can then be pushed and referenced from a BOM

This is intentionally a walkthrough of the current repo behavior, not a future
design sketch.

## Related Files

- Package directory:
  [scripts/poc/minimal-single-node/packages/cilium](../../scripts/poc/minimal-single-node/packages/cilium)
- Package manifest:
  [scripts/poc/minimal-single-node/packages/cilium/package.yaml](../../scripts/poc/minimal-single-node/packages/cilium/package.yaml)
- Default values file:
  [scripts/poc/minimal-single-node/packages/cilium/files/values/basic.yaml](../../scripts/poc/minimal-single-node/packages/cilium/files/values/basic.yaml)
- Tracked Cilium manifest:
  [scripts/poc/minimal-single-node/packages/cilium/manifests/cilium.yaml](../../scripts/poc/minimal-single-node/packages/cilium/manifests/cilium.yaml)
- Healthcheck hook:
  [scripts/poc/minimal-single-node/packages/cilium/hooks/healthcheck.sh](../../scripts/poc/minimal-single-node/packages/cilium/hooks/healthcheck.sh)
- OCI packaging CLI:
  [cmd/sealos/cmd/sync_package.go](../../cmd/sealos/cmd/sync_package.go)
- PoC asset fetcher:
  [scripts/poc/minimal-single-node/fetch-assets.sh](../../scripts/poc/minimal-single-node/fetch-assets.sh)
- PoC asset stager:
  [scripts/poc/minimal-single-node/stage-assets.sh](../../scripts/poc/minimal-single-node/stage-assets.sh)
- PoC OCI publisher:
  [scripts/poc/minimal-single-node/publish-oci.sh](../../scripts/poc/minimal-single-node/publish-oci.sh)

## What The Current Cilium Package Contains

Today the package is a small manifest-oriented application package:

```text
scripts/poc/minimal-single-node/packages/cilium/
  package.yaml
  files/values/basic.yaml
  manifests/cilium.yaml
  hooks/healthcheck.sh
```

The important parts of the package manifest are:

- `spec.class: application`
- one dependency on Kubernetes
- one declared input surface: `cilium-values`
- one manifest payload: `manifests/cilium.yaml`
- one packaged values file: `files/values/basic.yaml`
- one healthcheck hook

The package does not currently carry:

- a Helm chart
- a rootfs payload
- a bootstrap hook

## Step 1: Start From The Package Directory

The package directory already exists in-tree:

```bash
scripts/poc/minimal-single-node/packages/cilium
```

The package manifest looks like this at a high level:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: cilium-cni
spec:
  component: cilium
  version: v1.15.0
  class: application
  dependencies:
    - name: kubernetes
      version: v1.30.3
  inputs:
    - name: cilium-values
      type: valuesFile
      path: files/values/basic.yaml
  contents:
    - name: cilium-manifests
      type: manifest
      path: manifests/cilium.yaml
    - name: cilium-values
      type: values
      path: files/values/basic.yaml
  hooks:
    - name: healthcheck
      phase: healthcheck
      target: cluster
      path: hooks/healthcheck.sh
```

This means the package contract says:

- Cilium is applied as cluster workload content
- Kubernetes must already exist
- the package carries one manifest payload and one default values payload
- the package exposes one values input surface for cluster-local binding

## Step 2: Prepare Or Refresh The Package Assets

The current repo supports two ways to populate the package payload.

### Option A: Use The Tracked Manifest Already In The Repo

For the PoC, the repository already tracks:

- `manifests/cilium.yaml`
- `files/values/basic.yaml`

That is enough to inspect, build, push, and render the package today.

### Option B: Regenerate The Manifest From Cilium CLI

The PoC helper script can regenerate the manifest from the checked-in values
file:

```bash
scripts/poc/minimal-single-node/fetch-assets.sh
```

What it does for Cilium today:

1. download `cilium` CLI
2. read `scripts/poc/minimal-single-node/packages/cilium/files/values/basic.yaml`
3. run `cilium install --dry-run --version v1.15.0 -f <values-file>`
4. write the rendered manifest to a file such as
   `artifacts/cilium/cilium.yaml`

If you want to stage that refreshed manifest into the package directory, use:

```bash
install -D -m 0644 /path/to/cilium.yaml \
  scripts/poc/minimal-single-node/packages/cilium/manifests/cilium.yaml
```

If you want to use the repo helper instead, note that
`scripts/poc/minimal-single-node/stage-assets.sh` is a full PoC asset stager,
not a Cilium-only helper. It validates the runtime and Kubernetes binary inputs
for the whole three-package PoC before staging Cilium assets.

## Step 3: Inspect The Package Metadata

Before building the image, inspect the package:

```bash
sealos sync package inspect \
  --package-dir scripts/poc/minimal-single-node/packages/cilium
```

This reads `package.yaml` and reports the package metadata that will drive the
OCI build, including:

- package name: `cilium-cni`
- component: `cilium`
- version: `v1.15.0`
- class: `application`

This is the fastest way to confirm that the package directory is structurally
valid before pushing it into the OCI flow.

## Step 4: Build The OCI Package Image

Build the package into an OCI image with the first-class CLI:

```bash
sealos sync package build \
  --package-dir scripts/poc/minimal-single-node/packages/cilium \
  --image localhost:5000/poc-minimal/cilium-cni:v1.15.0 \
  --platform linux/amd64 \
  --timestamp 0 \
  --distribution poc-minimal
```

What the build command does today:

1. load package metadata from `package.yaml`
2. stage the package directory into a deterministic build context
3. write a minimal `Containerfile` that copies the package tree into `/`
4. build an OCI image from `scratch`
5. attach package metadata as OCI labels

Important labels applied by the build command include:

- `sealos.io.type=application`
- `sealos.io.version=v1.15.0`
- `distribution.sealos.io/kind=ComponentPackage`
- `distribution.sealos.io/package-name=cilium-cni`
- `distribution.sealos.io/component=cilium`

At this point the image exists locally, but the BOM still needs a pushed image
reference and digest.

## Step 5: Push The OCI Package Image

Push the previously built image:

```bash
sealos sync package push \
  --image localhost:5000/poc-minimal/cilium-cni:v1.15.0 \
  --destination localhost:5000/poc-minimal/cilium-cni:v1.15.0 \
  --provenance-file /tmp/cilium-cni.provenance.yaml
```

The push command returns the pushed image digest and the fully qualified
reference. It validates the digest as an OCI digest before reporting success,
and the optional provenance file records the destination transport, digest
algorithm, encoded digest value, and which registry auth inputs were configured.
Credential values passed through `--creds` are forwarded to the underlying push
but are not written to command output, provenance, or failure diagnostics.

That digest is what should be recorded in a BOM. Signing remains an external
registry/image-policy concern for this PoC flow; the BOM pins package selection
by image plus digest.

For a mirror or air-gapped registry, point `--destination` at the mirror
registry and record that mirror image plus digest in the BOM. Render-time OCI
resolution still uses the digest-derived pull-if-missing cache; cache GC,
prewarming, and registry-outage runbooks are separate operational tasks, not
part of package publishing.

The PoC wrapper script that does this for all three components is:

```bash
scripts/poc/minimal-single-node/publish-oci.sh \
  --registry-prefix localhost:5000/poc-minimal
```

Internally, that script:

1. stages package assets
2. runs `sealos sync package inspect`
3. runs `sealos sync package build`
4. runs `sealos sync package push`
5. writes an OCI-backed BOM

## Step 6: Reference Cilium From A BOM

After push, the BOM entry for Cilium should look like this:

```yaml
- name: cilium
  kind: infra
  version: v1.15.0
  dependencies:
    - kubernetes
  artifact:
    name: cilium-cni
    image: localhost:5000/poc-minimal/cilium-cni:v1.15.0
    digest: sha256:<pushed-digest>
```

The important point is that the BOM references the package artifact by both:

- image
- digest

That keeps package selection deterministic.

## Step 7: Render The Bundle

There are two useful ways to render the PoC bundle.

### Render From Local Package Directories

Use the helper wrapper:

```bash
scripts/poc/minimal-single-node/render.sh --package-mode local
```

In local mode, the script passes `--package-source` overrides for:

- `containerd`
- `kubernetes`
- `cilium`

This is the easiest development loop when you are iterating on the package
directory itself.

### Render From OCI Package Images

After publishing OCI images and generating the OCI-backed BOM:

```bash
scripts/poc/minimal-single-node/render.sh --package-mode oci
```

In OCI mode, the render path resolves Cilium from the BOM artifact reference
instead of a local `--package-source` override.

The rendered bundle currently carries Cilium payloads under
`components/cilium/`, with package-relative content copied under
`components/cilium/files/`. In practice that means paths such as:

```text
components/cilium/
  package.yaml
  files/manifests/cilium.yaml
  files/files/values/basic.yaml
  files/hooks/healthcheck.sh
```

That is the filesystem-backed desired-state artifact that later apply logic
consumes.

## Step 8: Understand What Is Global Vs Local In This Package

For the current Cilium package:

- `global`
  - `package.yaml`
  - `manifests/cilium.yaml`
  - `hooks/healthcheck.sh`
  - the packaged baseline `files/values/basic.yaml`
  - package identity, compatibility, and dependency metadata
- `local`
  - the concrete values bound to the `cilium-values` input surface during
    hydration
  - cluster-specific IPAM or routing values
  - environment-specific registry or mirror settings
  - other approved per-cluster overrides

One subtle but important rule applies here:

`files/values/basic.yaml` is both:

- packaged content
- an input surface

That does not make the file itself local. The packaged file is still a
package-owned default or merge base. What becomes local is the actual value
bound at hydration time.

## Step 9: Know The Current Limits

This walkthrough intentionally describes the current implementation, which has a
few important limits:

- the package is currently manifest-based, not chart-based
- the values file is packaged and carried through the bundle, but current repo
  behavior should be read as “declared input surface plus packaged default,” not
  as a generic Helm rendering engine
- the end-to-end PoC still includes the other two packages, so a realistic
  render or apply path usually runs in the context of the full three-component
  BOM

## Bottom Line

Today, Cilium packaging in this repo means:

1. keep a `ComponentPackage` directory under
   `scripts/poc/minimal-single-node/packages/cilium`
2. populate it with a tracked or regenerated Cilium manifest, default values,
   and a healthcheck hook
3. inspect it with `sealos sync package inspect`
4. build it into an OCI image with `sealos sync package build`
5. push it with `sealos sync package push`
6. reference the pushed image and digest from a BOM
7. render the final bundle from either local package directories or OCI package
   artifacts
