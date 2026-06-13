# CI/CD bootstrap — GitHub Actions secrets and in-cluster ghcr.io pull credentials.
# Run `terraform apply` once after setting up a new cluster or rotating credentials.

locals {
  # Namespaces that run app workloads and need to pull from ghcr.io.
  app_namespaces = ["finance", "auth"]

  # Base64-encoded kubeconfig for the GitHub Actions runner.
  # We reuse the same kubeconfig that Terraform itself reads, but with the
  # external server address so GitHub runners can reach the cluster.
  kubeconfig_b64 = base64encode(file(pathexpand("~/.kube/config")))
}

# ---------------------------------------------------------------------------
# GitHub Actions secrets
# ---------------------------------------------------------------------------

data "github_repository" "homelab" {
  name = "homelab"
}

resource "github_actions_secret" "kubeconfig" {
  repository      = data.github_repository.homelab.name
  secret_name     = "KUBECONFIG"
  plaintext_value = local.kubeconfig_b64
}

# ---------------------------------------------------------------------------
# ghcr.io pull secret — created in every app namespace
# ---------------------------------------------------------------------------

resource "kubernetes_secret" "ghcr_credentials" {
  for_each = toset(local.app_namespaces)

  metadata {
    name      = "ghcr-credentials"
    namespace = kubernetes_namespace.domains[each.key].metadata[0].name
  }

  type = "kubernetes.io/dockerconfigjson"

  data = {
    ".dockerconfigjson" = jsonencode({
      auths = {
        "ghcr.io" = {
          username = var.github_owner
          password = var.github_token
          auth     = base64encode("${var.github_owner}:${var.github_token}")
        }
      }
    })
  }
}
