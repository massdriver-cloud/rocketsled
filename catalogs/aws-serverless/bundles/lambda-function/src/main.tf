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

# Stand-in for the real Lambda runtime. It reads its connections the way the
# real module does so the wiring is exercised, and creates nothing in AWS.

resource "random_pet" "function" {
  keepers = {
    image_uri = var.image_uri
    api       = var.api.id
    routes    = jsonencode(var.routes)
  }
}

locals {
  function_name = "${var.md_metadata.name_prefix}-${random_pet.function.id}"
  table_name    = try(var.storage.name, null)
  in_network    = try(var.network.id, null) != null
  image         = var.image_uri != "" ? var.image_uri : "placeholder"
}

output "function_name" {
  value = local.function_name
}

output "invoke_url" {
  value = var.api.endpoint
}

output "image" {
  value = local.image
}
