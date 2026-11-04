# Rocketsled ideas

## Tenant isolation: environment defaults per stack instance

Today every stack instance is a fork of the project's base environment with
`copyEnvironmentDefaults`, so each instance inherits the same provisioning
credentials, VPC, and cluster as the base environment.

Ideas, in order of effort:

1. **Hard-coded defaults in config.** Add an `environment_defaults` list per
   project in `rocketsled.yaml` (resource IDs: IAM role, VPC, subnet). Rocketsled
   calls `setEnvironmentDefault` for each one right after `forkEnvironment` on
   every stack instance. Cheap, deterministic, and enough for a single-tenant
   platform team.
2. **Per-stack defaults.** Same mechanism, but the resource IDs live on the
   Rocketsled stack record instead of config, so an operator can pin one stack
   (customer) to its own role/VPC while the rest share the base environment's.
   `example/catalog/aws-eks-3-tier/bundles/k8s-landing-zone` is the Kubernetes shape of
   this: one deploy per customer yields a namespaced `kubernetes-cluster` credential
   that is exactly the resource to pin here.
3. **Org factory / token vending.** When a stack is created, Rocketsled provisions
   a tenant boundary: vend an IAM role scoped to that tenant for provisioning,
   create a VPC or a locked-down subnet per tenant (could itself be a Massdriver
   environment in a "tenancy" project), import the outputs as resources, then
   attach them as environment defaults to all of that tenant's stack instances.
   Makes Rocketsled multi-tenant with hard blast-radius boundaries, and lets the
   platform team meter or revoke a tenant by rotating the vended role.
