terraform {
  required_version = ">= 1.5"
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.30"
    }
    massdriver = {
      source  = "massdriver-cloud/massdriver"
      version = "~> 2.0"
    }
  }
}

provider "kubernetes" {
  host                   = var.kubernetes_cluster.authentication.cluster.server
  cluster_ca_certificate = base64decode(var.kubernetes_cluster.authentication.cluster["certificate-authority-data"])
  token                  = var.kubernetes_cluster.authentication.user.token
}

locals {
  labels = merge(var.md_metadata.default_tags, {
    "app.kubernetes.io/managed-by" = "massdriver"
    "rocketsled.massdriver.cloud/landing-zone" = var.md_metadata.name_prefix
  })
}

resource "kubernetes_namespace" "tenant" {
  metadata {
    name   = var.namespace
    labels = local.labels
  }
}

resource "kubernetes_resource_quota" "tenant" {
  metadata {
    name      = "tenant"
    namespace = kubernetes_namespace.tenant.metadata[0].name
  }
  spec {
    hard = {
      "requests.cpu"    = var.quota.cpu
      "requests.memory" = var.quota.memory
      "limits.cpu"      = var.quota.cpu
      "limits.memory"   = var.quota.memory
      "pods"            = tostring(var.quota.pods)
    }
  }
}

# Containers that declare no resources still count against the quota.
resource "kubernetes_limit_range" "tenant" {
  metadata {
    name      = "defaults"
    namespace = kubernetes_namespace.tenant.metadata[0].name
  }
  spec {
    limit {
      type = "Container"
      default = {
        cpu    = "250m"
        memory = "256Mi"
      }
      default_request = {
        cpu    = "100m"
        memory = "128Mi"
      }
    }
  }
}

resource "kubernetes_network_policy" "isolate" {
  count = var.isolate_network ? 1 : 0
  metadata {
    name      = "isolate-namespace"
    namespace = kubernetes_namespace.tenant.metadata[0].name
  }
  spec {
    pod_selector {}
    policy_types = ["Ingress"]
    ingress {
      from {
        pod_selector {}
      }
    }
    # Load balancer traffic arrives from node IPs, not pods, so allow it.
    ingress {
      from {
        ip_block {
          cidr = "0.0.0.0/0"
        }
      }
    }
  }
}

# --- Deployer credential -----------------------------------------------------

resource "kubernetes_service_account" "deployer" {
  metadata {
    name      = "deployer"
    namespace = kubernetes_namespace.tenant.metadata[0].name
    labels    = local.labels
  }
}

resource "kubernetes_role" "deployer" {
  metadata {
    name      = "deployer"
    namespace = kubernetes_namespace.tenant.metadata[0].name
  }
  rule {
    api_groups = ["", "apps", "batch", "autoscaling", "networking.k8s.io", "policy"]
    resources  = ["*"]
    verbs      = ["*"]
  }
}

resource "kubernetes_role_binding" "deployer" {
  metadata {
    name      = "deployer"
    namespace = kubernetes_namespace.tenant.metadata[0].name
  }
  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "Role"
    name      = kubernetes_role.deployer.metadata[0].name
  }
  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.deployer.metadata[0].name
    namespace = kubernetes_namespace.tenant.metadata[0].name
  }
}

resource "kubernetes_secret" "deployer_token" {
  metadata {
    name      = "deployer-token"
    namespace = kubernetes_namespace.tenant.metadata[0].name
    annotations = {
      "kubernetes.io/service-account.name" = kubernetes_service_account.deployer.metadata[0].name
    }
  }
  type                           = "kubernetes.io/service-account-token"
  wait_for_service_account_token = true
}
