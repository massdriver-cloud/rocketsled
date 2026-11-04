# aws-eks-3-tier

A catalog for running Rocketsled stacks on Kubernetes. Same layout as
[massdriver-catalog](https://github.com/massdriver-cloud/massdriver-catalog):
`platforms/` for credential types, `resource-types/` for the contracts between
bundles, `bundles/` for the infrastructure and application code.

```
platform team                       tenant (one per customer)             stack instance (forked per branch)
─────────────────────────────       ──────────────────────────────        ──────────────────────────────────────────
aws-vpc ─virtual-network─▶ aws-eks ─kubernetes-cluster (admin)─▶ k8s-landing-zone ─kubernetes-cluster (namespaced)─▶ k8s-postgres ─postgres-database─┐
                                                                                                                  ├▶ k8s-mariadb ──mysql-database───┼▶ k8s-app ─workload
                                                                                                                  └▶ k8s-blob-storage ─object-storage┘
```

## Bundles

| Bundle | Provisioner | Consumes | Produces |
|---|---|---|---|
| `aws-vpc` | OpenTofu | `aws-iam-role` | `virtual-network` |
| `aws-eks` | OpenTofu | `aws-iam-role`, `virtual-network` | `kubernetes-cluster` (cluster-admin) |
| `k8s-landing-zone` | OpenTofu | `kubernetes-cluster` (admin) | `kubernetes-cluster` (namespaced deployer) |
| `k8s-postgres` | Helm | `kubernetes-cluster` | `postgres-database` |
| `k8s-mariadb` | Helm | `kubernetes-cluster` | `mysql-database` |
| `k8s-blob-storage` | Helm | `kubernetes-cluster` | `object-storage` (MinIO, with static keys) |
| `k8s-app` | Helm | `kubernetes-cluster`, `postgres-database`, `mysql-database`, `object-storage` | `workload` |

The four Helm bundles consume only the `kubernetes-cluster` platform type. They
never see AWS. Each one installs into the namespace carried by that credential,
so the same bundles run in any tenant, on any cluster, with no per-tenant code.

## Two projects, one platform

**Platform project**: `aws-vpc` → `aws-eks` → one `k8s-landing-zone` per tenant.
Deployed once by the platform team. Each landing zone's namespaced credential is
a resource that can be pinned as an environment default.

**Application project** (what Rocketsled links to): `k8s-postgres`, `k8s-mariadb`,
`k8s-blob-storage`, `k8s-app`, with `kubernetes_cluster` left unlinked on every
component so it is satisfied by an environment default. Its base environment
carries a shared sandbox tenant's credential as that default. When Rocketsled
forks the base environment for a stack instance, the fork copies the default. To
give a customer their own tenant, set their landing zone's credential as the
default on their instances instead. Doing that from Rocketsled config is tracked
in the repository's TODO.md.

Rocketsled config for the application project:

```yaml
- id: threetier
  base_environment: base
  app_component: app
  image_tag_param: image.tag
  image_registry: "123456789012.dkr.ecr.us-west-2.amazonaws.com/{{stack}}"
```

## Publish

```sh
make publish-platforms        # aws-iam-role, kubernetes-cluster
make publish-resource-types   # virtual-network, postgres-database, mysql-database, object-storage, workload
make publish-bundles          # every bundle, development channel
```

Promoting a bundle to a stable release is a human step: publish that one bundle
by hand without the development flag.

`make lint` and `make validate` run `mass bundle lint`, `helm lint`, `helm template`
and `tofu validate` locally. Bundles reference resource types by name, so the
types must be published to your organization before `mass bundle build` can
resolve them.

## What is deliberately simple

The data-tier Helm bundles are single-node with no backups. They exist so a
branch can get a complete, isolated copy of the stack in seconds. Production
environments should swap in managed services that emit the same resource types;
`k8s-app` will not notice.
