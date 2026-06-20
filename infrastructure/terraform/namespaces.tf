locals {
  namespaces = concat(
    ["auth", "home", "finance", "test", "monitoring", "infrastructure"],
    var.enable_gitea ? ["gitea"] : []
  )
}

resource "kubernetes_namespace" "domains" {
  for_each = toset(local.namespaces)

  metadata {
    name = each.value
  }
}
