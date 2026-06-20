locals {
  namespaces = ["auth", "home", "finance", "test", "monitoring", "infrastructure"]
}

resource "kubernetes_namespace" "domains" {
  for_each = toset(local.namespaces)

  metadata {
    name = each.value
  }
}
