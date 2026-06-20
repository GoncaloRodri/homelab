resource "kubernetes_secret" "gitea_admin" {
  metadata {
    name      = "gitea-admin"
    namespace = kubernetes_namespace.domains["gitea"].metadata[0].name
  }
  data = {
    username = "admin"
    password = var.gitea_admin_password
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

    "postgresql-ha" = { enabled = false }
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

# imagePullSecret for finance namespace — allows k8s to pull images from Gitea registry.
# Containerd mirrors "git.homelab.local" to localhost:30002 (see k3d/config.yaml) and
# forwards these credentials to authenticate against the Gitea NodePort.
resource "kubernetes_secret" "gitea_registry_finance" {
  metadata {
    name      = "gitea-registry"
    namespace = kubernetes_namespace.domains["finance"].metadata[0].name
  }
  type = "kubernetes.io/dockerconfigjson"
  data = {
    ".dockerconfigjson" = jsonencode({
      auths = {
        "git.homelab.local" = {
          auth = base64encode("admin:${var.gitea_admin_password}")
        }
      }
    })
  }
}
