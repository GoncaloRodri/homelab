resource "random_password" "gitea_admin" {
  length  = 24
  special = false
}

resource "kubernetes_secret" "gitea_admin" {
  metadata {
    name      = "gitea-admin"
    namespace = kubernetes_namespace.domains["gitea"].metadata[0].name
  }
  data = {
    username = "admin"
    password = random_password.gitea_admin.result
    email    = "admin@homelab.local"
  }
}

resource "helm_release" "gitea" {
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
        existingSecret = kubernetes_secret.gitea_admin.metadata[0].name
      }
      config = {
        APP_NAME = "Homelab Git"
        server = {
          DOMAIN     = "git.homelab.local"
          ROOT_URL   = "http://git.homelab.local"
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
      hosts = [{
        host  = "git.homelab.local"
        paths = [{ path = "/", pathType = "Prefix" }]
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

# Placeholder secret created by Terraform; data is populated by the
# terraform_data bootstrapper below after Gitea is reachable.
resource "kubernetes_secret" "gitea_runner_token" {
  metadata {
    name      = "gitea-runner-token"
    namespace = kubernetes_namespace.domains["gitea"].metadata[0].name
  }
  data = { token = "" }

  lifecycle {
    # After the bootstrapper writes the real token we must not overwrite it
    # with the empty placeholder on subsequent applies.
    ignore_changes = [data]
  }
}

# On first apply: poll until Gitea is up, call the admin API to obtain a
# runner registration token, and patch the secret in place.
# On subsequent applies this resource is a no-op (terraform_data only
# re-runs its provisioner when triggers_replace changes).
resource "terraform_data" "gitea_runner_registration" {
  depends_on = [helm_release.gitea, kubernetes_secret.gitea_runner_token]

  # Re-bootstrap only if the admin password rotates.
  triggers_replace = [random_password.gitea_admin.id]

  provisioner "local-exec" {
    interpreter = ["/bin/sh", "-c"]
    command     = <<-EOT
      set -e

      echo "Waiting for Gitea to be ready..."
      until curl -sf "http://git.homelab.local/api/v1/version" > /dev/null 2>&1; do
        sleep 5
      done

      PASSWORD=$(kubectl get secret gitea-admin -n gitea \
        -o jsonpath='{.data.password}' | base64 -d)

      TOKEN=$(curl -sf \
        -u "admin:$PASSWORD" \
        "http://git.homelab.local/api/v1/admin/runners/registration-token" \
        | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

      kubectl patch secret gitea-runner-token -n gitea \
        -p "{\"data\":{\"token\":\"$(printf '%s' "$TOKEN" | base64)\"}}"

      echo "Runner registration token written to gitea-runner-token secret."
    EOT
  }
}

# imagePullSecret for all app namespaces — allows k8s to pull images from the
# local Gitea registry. Containerd mirrors "git.homelab.local" to localhost:30002
# (see k3d/config.yaml) and forwards these credentials to authenticate.
locals {
  app_namespaces = ["auth", "finance", "home", "test"]
}

resource "kubernetes_secret" "gitea_registry" {
  for_each = toset(local.app_namespaces)

  metadata {
    name      = "gitea-registry"
    namespace = kubernetes_namespace.domains[each.value].metadata[0].name
  }
  type = "kubernetes.io/dockerconfigjson"
  data = {
    ".dockerconfigjson" = jsonencode({
      auths = {
        "git.homelab.local" = {
          auth = base64encode("admin:${random_password.gitea_admin.result}")
        }
      }
    })
  }
}
