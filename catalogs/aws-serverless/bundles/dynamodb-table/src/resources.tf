resource "massdriver_resource" "table" {
  field = "table"
  name  = "Table ${local.name}"

  resource = jsonencode(merge(
    {
      name          = local.name
      arn           = local.arn
      region        = var.region
      partition_key = var.partition_key
      policies = [
        { name = "read", arn = "arn:aws:iam::${local.account_id}:policy/${local.name}-read" },
        { name = "read-write", arn = "arn:aws:iam::${local.account_id}:policy/${local.name}-read-write" },
      ]
    },
    var.sort_key == null ? {} : { sort_key = var.sort_key },
  ))
}
