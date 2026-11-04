resource "massdriver_resource" "kubernetes_cluster" {
  field = "kubernetes_cluster"
  name  = "Namespace ${var.namespace} on ${try(var.kubernetes_cluster.id, "cluster")}"

  resource = jsonencode({
    id        = "${try(var.kubernetes_cluster.id, "cluster")}-${var.namespace}"
    namespace = kubernetes_namespace.tenant.metadata[0].name
    authentication = {
      cluster = var.kubernetes_cluster.authentication.cluster
      user = {
        token = kubernetes_secret.deployer_token.data["token"]
      }
    }
  })
}
