resource "kubernetes_service_account" "act_runner" {
  metadata {
    name      = "act-runner"
    namespace = kubernetes_namespace.domains["gitea"].metadata[0].name
  }
}

resource "kubernetes_cluster_role" "act_runner" {
  metadata {
    name = "act-runner"
  }
  # Allow deploying to finance namespace
  rule {
    api_groups = ["apps"]
    resources  = ["deployments"]
    verbs      = ["get", "list", "patch", "update"]
  }
  rule {
    api_groups = [""]
    resources  = ["pods", "pods/log"]
    verbs      = ["get", "list"]
  }
  # Allow creating Kaniko build jobs in gitea namespace
  rule {
    api_groups = ["batch"]
    resources  = ["jobs"]
    verbs      = ["create", "get", "list", "watch", "delete"]
  }
}

resource "kubernetes_cluster_role_binding" "act_runner" {
  metadata {
    name = "act-runner"
  }
  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = kubernetes_cluster_role.act_runner.metadata[0].name
  }
  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.act_runner.metadata[0].name
    namespace = kubernetes_namespace.domains["gitea"].metadata[0].name
  }
}

# ConfigMap for act runner config (host executor mode — steps run directly in runner container)
resource "kubernetes_config_map" "act_runner" {
  metadata {
    name      = "act-runner-config"
    namespace = kubernetes_namespace.domains["gitea"].metadata[0].name
  }
  data = {
    "config.yaml" = yamlencode({
      log = { level = "info" }
      runner = {
        capacity           = 2
        fetch_timeout      = "5s"
        fetch_interval     = "2s"
        report_interval    = "1s"
        envs               = {}
      }
      cache = { enabled = false }
      container = {
        network         = "host"
        # Allow pipeline steps to mount the SA token for kubectl
        valid_volumes = [
          "/var/run/secrets/kubernetes.io/serviceaccount",
        ]
        docker_host = "tcp://localhost:2375"
      }
    })
  }
}

resource "kubernetes_deployment" "act_runner" {
  depends_on = [helm_release.gitea, kubernetes_secret.gitea_runner_token]

  metadata {
    name      = "act-runner"
    namespace = kubernetes_namespace.domains["gitea"].metadata[0].name
    labels    = { app = "act-runner" }
  }

  spec {
    replicas = 1
    selector {
      match_labels = { app = "act-runner" }
    }
    template {
      metadata {
        labels = { app = "act-runner" }
      }
      spec {
        service_account_name = kubernetes_service_account.act_runner.metadata[0].name

        # act runner — runs steps using Docker (provided by dind sidecar)
        container {
          name  = "runner"
          image = "gitea/act_runner:latest"

          command = ["/bin/sh", "-c"]
          args = [<<-EOT
            set -e
            # Register if not yet registered
            if [ ! -f /data/.runner ]; then
              act_runner register \
                --no-interactive \
                --instance http://gitea-http.gitea.svc.cluster.local:3000 \
                --token "$(cat /etc/runner-token/token)" \
                --name "k3d-runner-$(hostname)" \
                --labels ubuntu-latest
            fi
            exec act_runner daemon --config /etc/act-runner/config.yaml
          EOT
          ]

          env {
            name  = "DOCKER_HOST"
            value = "tcp://localhost:2375"
          }
          # Make the runner's KUBERNETES_SERVICE env accessible to pipeline steps
          env {
            name = "KUBERNETES_SERVICE_HOST"
            value_from {
              field_ref { field_path = "status.hostIP" }
            }
          }

          volume_mount {
            name       = "runner-data"
            mount_path = "/data"
          }
          volume_mount {
            name       = "runner-config"
            mount_path = "/etc/act-runner"
          }
          volume_mount {
            name       = "runner-token"
            mount_path = "/etc/runner-token"
            read_only  = true
          }

          resources {
            requests = { cpu = "100m", memory = "128Mi" }
            limits   = { cpu = "500m", memory = "512Mi" }
          }
        }

        # Docker-in-Docker: provides a Docker daemon for the pipeline steps
        container {
          name  = "dind"
          image = "docker:27-dind"

          security_context {
            privileged = true
          }
          args = [
            "--insecure-registry=gitea-http.gitea.svc.cluster.local:3000",
          ]
          env {
            name  = "DOCKER_TLS_CERTDIR"
            value = ""
          }

          volume_mount {
            name       = "docker-storage"
            mount_path = "/var/lib/docker"
          }

          resources {
            requests = { cpu = "200m", memory = "256Mi" }
            limits   = { cpu = "1", memory = "1Gi" }
          }
        }

        volume {
          name = "runner-data"
          empty_dir {}
        }
        volume {
          name = "docker-storage"
          empty_dir {}
        }
        volume {
          name = "runner-config"
          config_map {
            name = kubernetes_config_map.act_runner.metadata[0].name
          }
        }
        volume {
          name = "runner-token"
          secret {
            secret_name = kubernetes_secret.gitea_runner_token.metadata[0].name
          }
        }
      }
    }
  }
}
