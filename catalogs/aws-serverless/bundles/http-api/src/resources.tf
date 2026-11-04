resource "massdriver_resource" "api" {
  field = "api"
  name  = "HTTP API ${var.md_metadata.name_prefix}"

  resource = jsonencode({
    id            = local.id
    region        = var.region
    endpoint      = "https://${local.id}.execute-api.${var.region}.amazonaws.com"
    execution_arn = "arn:aws:execute-api:${var.region}:${local.account_id}:${local.id}"
  })
}
