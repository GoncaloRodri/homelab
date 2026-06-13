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
  }
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
