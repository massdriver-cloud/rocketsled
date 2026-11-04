resource "massdriver_resource" "kubernetes_cluster" {
  field = "kubernetes_cluster"
  name  = "EKS ${local.name} (cluster admin)"

  resource = jsonencode({
    id        = aws_eks_cluster.main.name
    namespace = "default"
    authentication = {
      cluster = {
        server                       = aws_eks_cluster.main.endpoint
        "certificate-authority-data" = aws_eks_cluster.main.certificate_authority[0].data
      }
      user = {
        token = kubernetes_secret.admin_token.data["token"]
      }
    }
  })
}
