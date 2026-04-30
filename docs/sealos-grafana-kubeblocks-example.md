# Design Example: Packaging Grafana With A KubeBlocks PostgreSQL Backend

## Status

Design example

## Summary

This document adds one concrete application example to the package-based
distribution model: Grafana as a user-facing observability panel with a
PostgreSQL backend managed through the KubeBlocks `Cluster` CRD.

The goal is not to describe an implementation that already exists in this
repository. The goal is to show how the current package, ownership, and local
binding model should handle:

- an application package with its own manifests
- a database lifecycle that should not be hidden inside the app package
- secret-bearing values that must remain cluster-local
- runtime-generated database credentials that should not be promoted into a
  shared baseline

## Related Documents

- Package contract:
  [sealos-component-package-format-design.md](./sealos-component-package-format-design.md)
- Ownership and drift model:
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- Top-level distribution model:
  [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md)
- Release-channel and derived-distribution model:
  [sealos-multi-cluster-release-and-promotion-design.md](./sealos-multi-cluster-release-and-promotion-design.md)

## External References

- KubeBlocks PostgreSQL quickstart:
  <https://kubeblocks.io/docs/preview/kubeblocks-for-postgresql/02-quickstart>
- KubeBlocks PostgreSQL custom secret:
  <https://kubeblocks.io/docs/preview/kubeblocks-for-postgresql/06-custom-secret/01-custom-secret>

The KubeBlocks docs above show the current external contract this example
assumes:

- PostgreSQL is represented by `apiVersion: apps.kubeblocks.io/v1`, `kind: Cluster`
- a PostgreSQL system account can be customized through
  `componentSpecs[].systemAccounts[].secretRef`
- KubeBlocks also creates its own runtime connection Secret for the account

## Design Intent

This example is meant to answer one narrow question:

How should Sealos package a stateful application when the application itself,
its database lifecycle, and its secrets do not all belong to the same ownership
surface?

The recommended answer is:

- do not hide Grafana, PostgreSQL, and secret material inside one opaque
  package
- model the database as its own package or component because it has storage,
  backup, upgrade, and blast-radius concerns of its own
- keep secret values in the local repo or another cluster-local secret path
- allow Grafana to consume secret references and connection endpoints without
  baking secret bytes into the shared artifact

## Recommended Component Split

For this example, the clean first-pass split is:

1. `kubeblocks-postgresql`
   A platform capability package, or an already-installed platform prerequisite,
   that provides the KubeBlocks operator and PostgreSQL addon support.
2. `grafana-db`
   An `application` package that carries the KubeBlocks `Cluster` manifest for
   the PostgreSQL backend used by Grafana.
3. `grafana`
   An `application` package that carries the Grafana Deployment, Service,
   Ingress, healthcheck logic, and config defaults.

This split follows the same package-boundary rule used elsewhere in the design:

- Grafana and PostgreSQL do not share one lifecycle
- PostgreSQL storage, retention, backup, sizing, and maintenance policy are
  not merely app-local knobs
- the database is therefore not just another values block inside the Grafana
  package

## Why Not One Opaque Grafana Package

Putting everything into one package would blur three different concerns:

- shared Grafana baseline content
- shared database baseline content
- cluster-local secret ownership

That shape usually causes two long-term problems:

- database policy starts leaking into local ad-hoc overrides instead of being
  tracked as a reviewable package contract
- secret-bearing data gets mixed into package payloads or promoted values files

The package model should resist both.

## Ownership Map

| Element | Recommended Ownership | Why |
| --- | --- | --- |
| Grafana manifests, service layout, healthcheck logic, default config structure | `global` | Shared application baseline that should be digest-pinned and reused. |
| KubeBlocks PostgreSQL `Cluster` manifest shape, default topology, default service version | `global` | Shared database baseline owned by the package revision. |
| Grafana ingress host, TLS Secret name, storage class choice, database Secret name, cluster-specific service overrides | `local` via declared inputs | These vary by environment but should stay within a defined binding surface. |
| Secret bytes for Grafana admin login and PostgreSQL account passwords | `local` | Secret-bearing values must stay cluster-local. |
| KubeBlocks-generated runtime account Secret | `local runtime state` | It is produced in-cluster and must not be promoted into shared artifacts. |
| Dashboards, alerting config, or plugin defaults intended for all clusters | `global` or separate patch package | These are shared platform opinions, not per-cluster secret state. |

## Package 1: `grafana-db`

The database package is responsible for the KubeBlocks `Cluster` resource and
its health semantics. It should not carry the actual password bytes.

### Suggested Package Manifest Shape

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: grafana-postgresql
spec:
  component: grafana-db
  version: 16.4.0-kb.1
  class: application
  dependencies:
    - name: kubeblocks-postgresql
  inputs:
    - name: grafana-db-values
      type: valuesFile
      path: files/values/basic.yaml
      required: true
  contents:
    - name: grafana-db-manifest
      type: manifest
      path: manifests/postgresql-cluster.yaml
    - name: grafana-db-values
      type: values
      path: files/values/basic.yaml
  hooks:
    - name: healthcheck
      phase: healthcheck
      target: cluster
      path: hooks/healthcheck.sh
```

The important point is that the packaged `basic.yaml` should carry defaults and
structure, not secret values.

### Suggested Baseline Values Shape

```yaml
clusterName: grafana-db
serviceVersion: "16.4.0"
storage:
  size: 20Gi
  className: ""
systemAccounts:
  postgres:
    secretName: grafana-db-root
```

The value file above contains only the name of the Secret to use. The Secret
itself remains local.

### Example KubeBlocks Cluster Payload

The rendered baseline manifest should eventually resolve to a KubeBlocks
`Cluster` similar to this:

```yaml
apiVersion: apps.kubeblocks.io/v1
kind: Cluster
metadata:
  name: grafana-db
spec:
  clusterDef: postgresql
  topology: replication
  terminationPolicy: Delete
  componentSpecs:
    - name: postgresql
      serviceVersion: 16.4.0
      replicas: 2
      systemAccounts:
        - name: postgres
          secretRef:
            name: grafana-db-root
```

The KubeBlocks CR is `global` package content. The referenced Secret object is
not.

### Local Secret For The Database Account

The local repo should carry the actual Secret bytes:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: grafana-db-root
type: Opaque
stringData:
  username: postgres
  password: <cluster-local-password>
```

This Secret is intentionally not part of the shared package artifact.

### Runtime Secret Boundary

KubeBlocks also creates its own runtime Secret for the account. In the official
PostgreSQL docs, the generated Secret name follows the pattern:

- `{cluster}-{component}-account-{name}`

For this example, the resulting runtime Secret name would typically look like:

- `grafana-db-postgresql-account-postgres`

That generated Secret is still `local`. It is not baseline package content and
should not be promoted upstream.

## Package 2: `grafana`

The Grafana package should carry the app manifests, the config baseline, and
healthcheck behavior, but not admin-password bytes or database-password bytes.

### Suggested Package Manifest Shape

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: grafana-observability
spec:
  component: grafana
  version: 11.0.0
  class: application
  dependencies:
    - name: grafana-db
  inputs:
    - name: grafana-values
      type: valuesFile
      path: files/values/basic.yaml
      required: true
  contents:
    - name: grafana-manifests
      type: manifest
      path: manifests/grafana.yaml
    - name: grafana-values
      type: values
      path: files/values/basic.yaml
  hooks:
    - name: healthcheck
      phase: healthcheck
      target: cluster
      path: hooks/healthcheck.sh
```

### Suggested Baseline Values Shape

```yaml
admin:
  existingSecret: grafana-admin-credentials
database:
  # Illustrative service name; exact generated service names follow
  # KubeBlocks naming conventions in the target release.
  host: grafana-db-postgresql-postgresql
  port: 5432
  name: grafana
  # Illustrative generated Secret name; exact names follow the operator's
  # runtime naming convention.
  credentialsSecretName: grafana-db-postgresql-account-postgres
service:
  type: ClusterIP
ingress:
  enabled: false
  host: ""
  tlsSecretName: ""
```

This keeps the package baseline explicit while leaving all secret-bearing bytes
outside the package.

### Local Secret For Grafana Admin

The Grafana admin login should also remain local:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin-credentials
type: Opaque
stringData:
  admin-user: admin
  admin-password: <cluster-local-password>
```

Again, the package may refer to this Secret by name, but it must not carry the
credential bytes itself.

## BOM Shape

A BOM that includes this example would typically look like:

```yaml
spec:
  components:
    - name: kubeblocks-postgresql
      kind: infra
      version: v1
      artifact:
        name: kubeblocks-postgresql
        image: registry.example/platform/kubeblocks-postgresql:v1
        digest: sha256:<digest>
    - name: grafana-db
      kind: infra
      version: 16.4.0-kb.1
      dependencies:
        - kubeblocks-postgresql
      artifact:
        name: grafana-postgresql
        image: registry.example/observability/grafana-postgresql:16.4.0-kb.1
        digest: sha256:<digest>
    - name: grafana
      kind: app
      version: 11.0.0
      dependencies:
        - grafana-db
      artifact:
        name: grafana-observability
        image: registry.example/observability/grafana-observability:11.0.0
        digest: sha256:<digest>
```

If the platform already guarantees that KubeBlocks PostgreSQL support is
present, the first component can be treated as a platform prerequisite rather
than something repeated in every app-focused BOM.

## What Is Global vs Local In This Example

### Global

- the decision to use Grafana
- the decision to use KubeBlocks PostgreSQL
- the package layout and manifests
- the chosen baseline service version
- healthcheck logic
- shared dashboard or sidecar defaults

### Local

- the name and bytes of `grafana-db-root`
- the name and bytes of `grafana-admin-credentials`
- ingress host and TLS Secret name
- storage class or storage size if it varies by cluster
- the concrete Secret reference bound into the package inputs

### Local Runtime

- KubeBlocks-generated account Secret contents
- live database state
- dashboards or settings created interactively after install, unless Sealos
  later chooses to manage them explicitly

## Promotion Rules

This example also shows what should and should not be promotable.

Promotable into shared baseline:

- a better Grafana baseline manifest
- a reviewed PostgreSQL topology change
- shared observability dashboards
- a healthcheck fix

Not promotable as shared baseline:

- one cluster's Grafana admin password
- one cluster's PostgreSQL password
- one cluster's generated runtime Secret

If many clusters want the same dashboard pack or the same retention and sizing
policy, that should become package content or a shared patch package. It should
not stay forever as many secret-bearing local copies.

## Practical Rule Of Thumb

For an app like Grafana with a database and secrets:

- package the app baseline
- package the database baseline separately
- keep secret names and endpoints in declared local input surfaces
- keep secret bytes in the local repo or another cluster-local secret system
- treat runtime-generated credentials as local runtime state, not as upstream
  package material

That is the cleanest way to preserve both reproducibility and secret boundary.
