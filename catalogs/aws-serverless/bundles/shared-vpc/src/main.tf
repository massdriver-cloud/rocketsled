terraform {
  required_version = ">= 1.5"
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
    massdriver = {
      source  = "massdriver-cloud/massdriver"
      version = "~> 2.0"
    }
  }
}

# Stand-in for the real VPC module: nothing is created in AWS, but the resource
# this bundle emits has the exact shape consumers expect.

resource "random_pet" "vpc" {
  keepers = {
    region = var.region
    cidr   = var.cidr
    zones  = var.zone_count
  }
}

resource "random_id" "vpc" {
  byte_length = 8
  keepers     = { pet = random_pet.vpc.id }
}

locals {
  zone_letters = ["a", "b", "c"]
  zones = [for i in range(var.zone_count) : {
    id   = "${var.region}${local.zone_letters[i]}"
    name = "${var.region}${local.zone_letters[i]}"
  }]
  subnets = flatten([for i in range(var.zone_count) : [
    { id = "subnet-${substr(random_id.vpc.hex, 0, 8)}${i}a", cidr = cidrsubnet(var.cidr, 4, i), availability_zone_id = local.zones[i].id, tier = "public" },
    { id = "subnet-${substr(random_id.vpc.hex, 0, 8)}${i}b", cidr = cidrsubnet(var.cidr, 4, i + 8), availability_zone_id = local.zones[i].id, tier = "private" },
  ]])
}
