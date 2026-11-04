# k8s-landing-zone

Takes a cluster-admin `kubernetes-cluster` credential (from `aws-eks`) and carves
out one tenant:

- a namespace with a `ResourceQuota` and a default `LimitRange`
- an optional `NetworkPolicy` that denies ingress from other namespaces
- a `deployer` service account with a namespace-scoped role and a long-lived token

It emits a **namespaced** `kubernetes-cluster` resource. Every Helm bundle in this
catalog consumes that shape, so a tenant's credential can be set as an environment
default and each stack instance forked from that environment deploys into the
tenant's namespace with the tenant's permissions and nothing more.

That is the hand-off Rocketsled needs for tenant isolation: one landing zone per
customer, its credential pinned to the customer's stack instances.
