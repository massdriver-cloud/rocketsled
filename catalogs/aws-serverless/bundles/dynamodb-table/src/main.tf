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

# Stand-in for the real DynamoDB module. The emitted resource matches the
# aws-dynamodb-table contract so the application bundle can be wired to it.

resource "random_pet" "table" {
  keepers = {
    region        = var.region
    partition_key = jsonencode(var.partition_key)
    sort_key      = jsonencode(var.sort_key)
  }
}

locals {
  name       = "${var.md_metadata.name_prefix}-${random_pet.table.id}"
  account_id = "123456789012"
  arn        = "arn:aws:dynamodb:${var.region}:${local.account_id}:table/${local.name}"
}
