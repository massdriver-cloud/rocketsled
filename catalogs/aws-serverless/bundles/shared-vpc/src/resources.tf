resource "massdriver_resource" "network" {
  field = "network"
  name  = "VPC ${var.md_metadata.name_prefix}"

  resource = jsonencode({
    id                 = "vpc-${random_id.vpc.hex}"
    region             = var.region
    cidr               = var.cidr
    availability_zones = local.zones
    subnets            = local.subnets
    internet_egress    = var.egress != "none"
  })
}
