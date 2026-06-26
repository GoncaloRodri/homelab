resource "random_password" "gitea_admin" {
  count   = var.enable_gitea ? 1 : 0
  length  = 24
  special = false
}

resource "kubernetes_secret" "gitea_admin" {
  count = var.enable_gitea ? 1 : 0
  metadata {
    name      = "gitea-admin"
    namespace = kubernetes_namespace.domains["gitea"].metadata[0].name
  }
  data = {
    username = "admin"
    password = random_password.gitea_admin[0].result
    email    = "admin@${var.domain}"
  }
}

resource "helm_release" "gitea" {
  count      = var.enable_gitea ? 1 : 0
  name       = "gitea"
  namespace  = kubernetes_namespace.domains["gitea"].metadata[0].name
  repository = "https://dl.gitea.com/charts/"
  chart      = "gitea"
  version    = "~> 12.0"
  atomic     = true
  timeout    = 300

  values = [yamlencode({
    gitea = {
      admin = {
        existingSecret = kubernetes_secret.gitea_admin[0].metadata[0].name
      }
      config = {
        APP_NAME = "Homelab Git"
        server = {
          DOMAIN     = "git.${var.domain}"
          ROOT_URL   = "${local.scheme}://git.${var.domain}"
          SSH_DOMAIN = "localhost"
          SSH_PORT   = 30001
        }
        database = { DB_TYPE = "sqlite3" }
        queue    = { TYPE = "level" }
        cache    = { ADAPTER = "memory" }
        session  = { PROVIDER = "memory" }
        packages = { ENABLED = "true" }
        service  = { DISABLE_REGISTRATION = "true" }
        log      = { LEVEL = "Warn" }
      }
    }

    "postgresql-ha"  = { enabled = false }
    "valkey-cluster" = { enabled = false }

    ingress = {
      enabled   = true
      className = "traefik"
      annotations = {
        "traefik.ingress.kubernetes.io/router.tls"              = "true"
        "traefik.ingress.kubernetes.io/router.tls.certresolver" = "letsencrypt"
      }
      hosts = [{
        host  = "git.${var.domain}"
        paths = [{ path = "/", pathType = "Prefix" }]
      }]
      tls = [{
        hosts = ["git.${var.domain}"]
      }]
    }

    # NodePort 30002: used by k3d containerd registry mirror (see k3d/config.yaml)
    service = {
      http = {
        type     = "NodePort"
        port     = 3000
        nodePort = 30002
      }
      ssh = {
        type     = "NodePort"
        port     = 22
        nodePort = 30001
      }
    }

    persistence = {
      enabled      = true
      size         = "10Gi"
      storageClass = "local-path"
    }

    resources = {
      requests = { cpu = "100m", memory = "256Mi" }
      limits   = { cpu = "500m", memory = "512Mi" }
    }
  })]
}

resource "kubernetes_secret" "gitea_runner_token" {
  count = var.enable_gitea ? 1 : 0
  metadata {
    name      = "gitea-runner-token"
    namespace = kubernetes_namespace.domains["gitea"].metadata[0].name
  }
  data = { token = "" }
  lifecycle {
    ignore_changes = [data]
  }
}

resource "terraform_data" "gitea_runner_registration" {
  count      = var.enable_gitea ? 1 : 0
  depends_on = [helm_release.gitea, kubernetes_secret.gitea_runner_token]

  triggers_replace = [random_password.gitea_admin[0].id]

  provisioner "local-exec" {
    interpreter = ["/bin/sh", "-c"]
    command     = <<-EOT
      set -e
      echo "Waiting for Gitea to be ready..."
      until curl -sf "${local.scheme}://git.${var.domain}/api/v1/version" > /dev/null 2>&1; do
        sleep 5
      done

      PASSWORD=$(kubectl get secret gitea-admin -n gitea \
        -o jsonpath='{.data.password}' | base64 -d)

      TOKEN=$(curl -sf \
        -u "admin:$PASSWORD" \
        "${local.scheme}://git.${var.domain}/api/v1/admin/runners/registration-token" \
        | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

      kubectl patch secret gitea-runner-token -n gitea \
        -p "{\"data\":{\"token\":\"$(printf '%s' "$TOKEN" | base64)\"}}"

      echo "Runner registration token written to gitea-runner-token secret."
    EOT
  }
}

locals {
  app_namespaces = ["auth", "finance", "home", "test"]
}

resource "kubernetes_secret" "gitea_registry" {
  for_each = var.enable_gitea ? toset(local.app_namespaces) : toset([])

  metadata {
    name      = "gitea-registry"
    namespace = kubernetes_namespace.domains[each.value].metadata[0].name
  }
  type = "kubernetes.io/dockerconfigjson"
  data = {
    ".dockerconfigjson" = jsonencode({
      auths = {
        "git.${var.domain}" = {
          auth = base64encode("admin:${random_password.gitea_admin[0].result}")
        }
      }
    })
  }
}
