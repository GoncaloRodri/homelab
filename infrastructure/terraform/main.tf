terraform {
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "2.32.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.17"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
    github = {
      source  = "integrations/github"
      version = "~> 6.0"
    }
  }
}

variable "github_token" {
  description = "GitHub PAT with repo and write:packages scopes (used for Actions secrets and ghcr.io pull)"
  type        = string
  sensitive   = true
}

variable "github_owner" {
  description = "GitHub username / org that owns the homelab repo"
  type        = string
  default     = "GoncaloRodri"
}

provider "github" {
  token = var.github_token
  owner = var.github_owner
}

locals {
  kubeconfig = yamldecode(file(pathexpand("~/.kube/config")))
  kubectx    = one([for c in local.kubeconfig.contexts : c if c.name == local.kubeconfig.current-context])
  kubecluster = one([for c in local.kubeconfig.clusters : c if c.name == local.kubectx.context.cluster])
  kubeuser   = one([for u in local.kubeconfig.users : u if u.name == local.kubectx.context.user])
  server     = replace(local.kubecluster.cluster.server, "0.0.0.0", "127.0.0.1")
}

provider "kubernetes" {
  host                   = local.server
  client_certificate     = base64decode(local.kubeuser.user.client-certificate-data)
  client_key             = base64decode(local.kubeuser.user.client-key-data)
  cluster_ca_certificate = base64decode(local.kubecluster.cluster.certificate-authority-data)
}

provider "helm" {
  kubernetes {
    host                   = local.server
    client_certificate     = base64decode(local.kubeuser.user.client-certificate-data)
    client_key             = base64decode(local.kubeuser.user.client-key-data)
    cluster_ca_certificate = base64decode(local.kubecluster.cluster.certificate-authority-data)
  }
}
