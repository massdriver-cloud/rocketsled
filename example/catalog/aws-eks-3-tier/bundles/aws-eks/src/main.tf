terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
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

locals {
  name            = var.md_metadata.name_prefix
  region          = var.network.region
  private_subnets = [for s in var.network.subnets : s.id if s.type == "private"]
}

provider "aws" {
  region = local.region
  assume_role {
    role_arn    = var.aws_authentication.arn
    external_id = try(var.aws_authentication.external_id, null)
  }
  default_tags {
    tags = var.md_metadata.default_tags
  }
}

# --- Control plane -----------------------------------------------------------

data "aws_iam_policy_document" "cluster_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["eks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "cluster" {
  name               = "${local.name}-cluster"
  assume_role_policy = data.aws_iam_policy_document.cluster_assume.json
}

resource "aws_iam_role_policy_attachment" "cluster" {
  for_each = toset([
    "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy",
    "arn:aws:iam::aws:policy/AmazonEKSVPCResourceController",
  ])
  role       = aws_iam_role.cluster.name
  policy_arn = each.value
}

resource "aws_cloudwatch_log_group" "cluster" {
  name              = "/aws/eks/${local.name}/cluster"
  retention_in_days = 30
}

resource "aws_kms_key" "secrets" {
  description             = "EKS secrets envelope encryption for ${local.name}"
  deletion_window_in_days = 7
  enable_key_rotation     = true
}

resource "aws_eks_cluster" "main" {
  name     = local.name
  version  = var.kubernetes_version
  role_arn = aws_iam_role.cluster.arn

  vpc_config {
    subnet_ids              = local.private_subnets
    endpoint_private_access = true
    # Massdriver's provisioners run outside the VPC and need the API endpoint.
    endpoint_public_access = true
    public_access_cidrs    = ["0.0.0.0/0"]
  }

  access_config {
    authentication_mode                         = "API_AND_CONFIG_MAP"
    bootstrap_cluster_creator_admin_permissions = true
  }

  encryption_config {
    provider {
      key_arn = aws_kms_key.secrets.arn
    }
    resources = ["secrets"]
  }

  enabled_cluster_log_types = ["api", "audit", "authenticator", "controllerManager", "scheduler"]

  depends_on = [
    aws_iam_role_policy_attachment.cluster,
    aws_cloudwatch_log_group.cluster,
  ]
}

# --- Nodes -------------------------------------------------------------------

data "aws_iam_policy_document" "node_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "node" {
  name               = "${local.name}-node"
  assume_role_policy = data.aws_iam_policy_document.node_assume.json
}

resource "aws_iam_role_policy_attachment" "node" {
  for_each = toset([
    "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
    "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
    "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
    "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore",
  ])
  role       = aws_iam_role.node.name
  policy_arn = each.value
}

resource "aws_eks_node_group" "main" {
  cluster_name    = aws_eks_cluster.main.name
  node_group_name = "${local.name}-default"
  node_role_arn   = aws_iam_role.node.arn
  subnet_ids      = local.private_subnets
  instance_types  = [var.nodes.instance_type]
  capacity_type   = var.nodes.capacity_type
  ami_type        = "AL2023_x86_64_STANDARD"

  scaling_config {
    min_size     = var.nodes.min_size
    desired_size = var.nodes.desired_size
    max_size     = var.nodes.max_size
  }

  update_config {
    max_unavailable = 1
  }

  lifecycle {
    # The cluster autoscaler owns desired_size after day one.
    ignore_changes = [scaling_config[0].desired_size]
  }

  depends_on = [aws_iam_role_policy_attachment.node]
}

resource "aws_eks_addon" "core" {
  for_each = toset(["vpc-cni", "coredns", "kube-proxy", "eks-pod-identity-agent"])

  cluster_name                = aws_eks_cluster.main.name
  addon_name                  = each.value
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "PRESERVE"

  depends_on = [aws_eks_node_group.main]
}

# --- Cluster admin credential ------------------------------------------------
# The creator role is admin (bootstrap_cluster_creator_admin_permissions). Use
# its short-lived token once to mint a long-lived service account token that
# the kubernetes-cluster resource carries.

data "aws_eks_cluster_auth" "main" {
  name = aws_eks_cluster.main.name
}

provider "kubernetes" {
  host                   = aws_eks_cluster.main.endpoint
  cluster_ca_certificate = base64decode(aws_eks_cluster.main.certificate_authority[0].data)
  token                  = data.aws_eks_cluster_auth.main.token
}

resource "kubernetes_service_account" "admin" {
  metadata {
    name      = "massdriver-admin"
    namespace = "kube-system"
  }
  depends_on = [aws_eks_node_group.main]
}

resource "kubernetes_cluster_role_binding" "admin" {
  metadata {
    name = "massdriver-admin"
  }
  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = "cluster-admin"
  }
  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.admin.metadata[0].name
    namespace = kubernetes_service_account.admin.metadata[0].namespace
  }
}

resource "kubernetes_secret" "admin_token" {
  metadata {
    name      = "massdriver-admin-token"
    namespace = "kube-system"
    annotations = {
      "kubernetes.io/service-account.name" = kubernetes_service_account.admin.metadata[0].name
    }
  }
  type                           = "kubernetes.io/service-account-token"
  wait_for_service_account_token = true
}
