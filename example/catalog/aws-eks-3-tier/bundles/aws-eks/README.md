# aws-eks

An EKS control plane plus one managed node group, placed in the private subnets
of the connected `virtual-network`. Secrets are envelope-encrypted with a
dedicated KMS key, control-plane logs go to CloudWatch, and the core add-ons
(vpc-cni, coredns, kube-proxy, pod identity agent) are managed by EKS.

The bundle emits a **cluster-admin** `kubernetes-cluster` resource backed by a
long-lived service account token. That credential is for platform bundles, most
importantly `k8s-landing-zone`, which mints the namespaced credentials that
application stacks actually receive.

Presets: **Development** runs two `t3.large` nodes; **Production** starts at
three `m6i.xlarge` with headroom to nine.
