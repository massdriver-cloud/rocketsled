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

# Stand-in for the real API Gateway module.

resource "random_pet" "api" {
  keepers = { region = var.region }
}

resource "random_id" "api" {
  byte_length = 5
  keepers     = { pet = random_pet.api.id }
}

locals {
  id         = random_id.api.hex
  account_id = "123456789012"
}
