# The first project in a new Rocketsled organization: one of everything the
# catalog offers, linked, with a Production environment to deploy into.

resource "massdriver_project" "serverless" {
  identifier  = "serverless"
  name        = "AWS Serverless Platform"
  description = "An HTTP API, a Lambda runtime, a DynamoDB table and a VPC. Built by Rocketsled."
}

resource "massdriver_environment" "production" {
  project_id = massdriver_project.serverless.id
  identifier = "production"
  name       = "Production"
}

locals {
  components = {
    vpc   = { bundle = "shared-vpc", name = "Network" }
    api   = { bundle = "http-api", name = "HTTP API" }
    table = { bundle = "dynamodb-table", name = "Table" }
    app   = { bundle = "lambda-function", name = "Application" }
  }

  links = {
    app_api     = { from = "api", from_field = "api", to = "app", to_field = "api" }
    app_storage = { from = "table", from_field = "table", to = "app", to_field = "storage" }
    app_network = { from = "vpc", from_field = "network", to = "app", to_field = "network" }
  }
}

resource "massdriver_component" "this" {
  for_each = local.components

  project_id  = massdriver_project.serverless.id
  identifier  = each.key
  name        = each.value.name
  bundle_name = each.value.bundle
}

resource "massdriver_component_link" "this" {
  for_each = local.links

  from_component_id = massdriver_component.this[each.value.from].id
  from_field        = each.value.from_field
  to_component_id   = massdriver_component.this[each.value.to].id
  to_field          = each.value.to_field
}
