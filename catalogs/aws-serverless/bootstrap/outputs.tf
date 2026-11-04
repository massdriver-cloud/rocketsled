output "project" {
  value = massdriver_project.serverless.id
}

output "environments" {
  value = { production = massdriver_environment.production.id }
}

output "components" {
  value = { for k, c in massdriver_component.this : k => c.id }
}
