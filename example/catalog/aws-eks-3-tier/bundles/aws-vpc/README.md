# aws-vpc

A VPC shaped for EKS. Two or three availability zones, each with one public and
one private /20 subnet, an internet gateway, and either no NAT, one NAT gateway,
or one per AZ. Subnets carry the `kubernetes.io/role/elb` and
`kubernetes.io/role/internal-elb` tags so load balancers land where they should.

Produces a `virtual-network` resource that `aws-eks` consumes.

Presets: **Development** keeps cost down with a single NAT; **Production** spreads
across three AZs with NAT per AZ and flow logs on.
