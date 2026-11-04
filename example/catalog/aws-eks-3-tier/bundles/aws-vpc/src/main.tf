terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    massdriver = {
      source  = "massdriver-cloud/massdriver"
      version = "~> 2.0"
    }
  }
}

provider "aws" {
  region = var.region
  assume_role {
    role_arn    = var.aws_authentication.arn
    external_id = try(var.aws_authentication.external_id, null)
  }
  default_tags {
    tags = var.md_metadata.default_tags
  }
}

data "aws_availability_zones" "available" {
  state = "available"
  filter {
    name   = "opt-in-status"
    values = ["opt-in-not-required"]
  }
}

locals {
  name = var.md_metadata.name_prefix
  azs  = slice(sort(data.aws_availability_zones.available.names), 0, var.availability_zones)

  # /16 -> /20 subnets: public in the low half, private in the high half.
  public_cidrs  = [for i in range(var.availability_zones) : cidrsubnet(var.cidr, 4, i)]
  private_cidrs = [for i in range(var.availability_zones) : cidrsubnet(var.cidr, 4, i + 8)]

  nat_count = var.nat_gateways == "none" ? 0 : (var.nat_gateways == "single" ? 1 : var.availability_zones)
}

resource "aws_vpc" "main" {
  cidr_block           = var.cidr
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = local.name }
}

resource "aws_default_security_group" "main" {
  vpc_id = aws_vpc.main.id
  # No rules: the default SG allows nothing, so every workload declares its own.
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = local.name }
}

resource "aws_subnet" "public" {
  count                   = var.availability_zones
  vpc_id                  = aws_vpc.main.id
  cidr_block              = local.public_cidrs[count.index]
  availability_zone       = local.azs[count.index]
  map_public_ip_on_launch = false
  tags = {
    Name                     = "${local.name}-public-${local.azs[count.index]}"
    "kubernetes.io/role/elb" = "1"
    type                     = "public"
  }
}

resource "aws_subnet" "private" {
  count             = var.availability_zones
  vpc_id            = aws_vpc.main.id
  cidr_block        = local.private_cidrs[count.index]
  availability_zone = local.azs[count.index]
  tags = {
    Name                              = "${local.name}-private-${local.azs[count.index]}"
    "kubernetes.io/role/internal-elb" = "1"
    type                              = "private"
  }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${local.name}-public" }
}

resource "aws_route" "public_internet" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.main.id
}

resource "aws_route_table_association" "public" {
  count          = var.availability_zones
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

resource "aws_eip" "nat" {
  count  = local.nat_count
  domain = "vpc"
  tags   = { Name = "${local.name}-nat-${count.index}" }
}

resource "aws_nat_gateway" "main" {
  count         = local.nat_count
  allocation_id = aws_eip.nat[count.index].id
  subnet_id     = aws_subnet.public[count.index].id
  tags          = { Name = "${local.name}-nat-${local.azs[count.index]}" }
  depends_on    = [aws_internet_gateway.main]
}

resource "aws_route_table" "private" {
  count  = var.availability_zones
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${local.name}-private-${local.azs[count.index]}" }
}

resource "aws_route" "private_nat" {
  count                  = local.nat_count == 0 ? 0 : var.availability_zones
  route_table_id         = aws_route_table.private[count.index].id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.main[min(count.index, local.nat_count - 1)].id
}

resource "aws_route_table_association" "private" {
  count          = var.availability_zones
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private[count.index].id
}

# --- Flow logs ---------------------------------------------------------------

resource "aws_cloudwatch_log_group" "flow" {
  count             = var.enable_flow_logs ? 1 : 0
  name              = "/aws/vpc/${local.name}/flow-logs"
  retention_in_days = 30
}

data "aws_iam_policy_document" "flow_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["vpc-flow-logs.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "flow_write" {
  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents", "logs:DescribeLogGroups", "logs:DescribeLogStreams"]
    resources = var.enable_flow_logs ? ["${aws_cloudwatch_log_group.flow[0].arn}:*"] : []
  }
}

resource "aws_iam_role" "flow" {
  count              = var.enable_flow_logs ? 1 : 0
  name               = "${local.name}-flow-logs"
  assume_role_policy = data.aws_iam_policy_document.flow_assume.json
}

resource "aws_iam_role_policy" "flow" {
  count  = var.enable_flow_logs ? 1 : 0
  name   = "write-flow-logs"
  role   = aws_iam_role.flow[0].id
  policy = data.aws_iam_policy_document.flow_write.json
}

resource "aws_flow_log" "main" {
  count                = var.enable_flow_logs ? 1 : 0
  vpc_id               = aws_vpc.main.id
  traffic_type         = "ALL"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow[0].arn
  iam_role_arn         = aws_iam_role.flow[0].arn
}
