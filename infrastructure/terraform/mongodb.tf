resource "random_password" "mongodb" {
  length  = 24
  special = false
}

resource "kubernetes_secret" "mongodb" {
  metadata {
    name      = "mongodb"
    namespace = kubernetes_namespace.domains["infrastructure"].metadata[0].name
  }

  data = {
    MONGO_INITDB_ROOT_PASSWORD = random_password.mongodb.result
    MONGO_URI                  = "mongodb://root:${random_password.mongodb.result}@mongodb.infrastructure.svc:27017"
    MONGO_DB                   = "homelab"
  }
}

resource "kubernetes_secret" "mongodb_shared_config" {
  for_each = toset(local.namespaces)
  metadata {
    name      = "mongodb-shared-config"
    namespace = kubernetes_namespace.domains[each.key].metadata[0].name
  }

  data = {
    MONGO_URI = "mongodb://root:${random_password.mongodb.result}@mongodb.infrastructure.svc:27017"
    MONGO_DB  = "homelab"
  }
}

resource "kubernetes_service" "mongodb" {
  metadata {
    name      = "mongodb"
    namespace = kubernetes_namespace.domains["infrastructure"].metadata[0].name
  }

  spec {
    selector = {
      app = "mongodb"
    }
    port {
      port        = 27017
      target_port = 27017
    }
  }
}

resource "kubernetes_stateful_set" "mongodb" {
  depends_on = [kubernetes_secret.mongodb, kubernetes_service.mongodb]

  wait_for_rollout = false

  metadata {
    name      = "mongodb"
    namespace = kubernetes_namespace.domains["infrastructure"].metadata[0].name
  }

  spec {
    service_name = "mongodb"
    replicas     = 1

    selector {
      match_labels = {
        app = "mongodb"
      }
    }

    template {
      metadata {
        labels = {
          app = "mongodb"
        }
      }

      spec {
        container {
          name  = "mongodb"
          image = "mongo:8"
          args  = ["--wiredTigerCacheSizeGB=0.25"]

          env {
            name  = "MONGO_INITDB_ROOT_USERNAME"
            value = "root"
          }
          env {
            name = "MONGO_INITDB_ROOT_PASSWORD"
            value_from {
              secret_key_ref {
                name = "mongodb"
                key  = "MONGO_INITDB_ROOT_PASSWORD"
              }
            }
          }
          env {
            name  = "MONGO_INITDB_DATABASE"
            value = "homelab"
          }

          volume_mount {
            name       = "mongodb-data"
            mount_path = "/data/db"
          }

          port {
            container_port = 27017
          }

          resources {
            requests = {
              cpu    = "200m"
              memory = "256Mi"
            }
            limits = {
              cpu    = "500m"
              memory = "512Mi"
            }
          }
        }
      }
    }

    volume_claim_template {
      metadata {
        name = "mongodb-data"
      }
      spec {
        access_modes       = ["ReadWriteOnce"]
        storage_class_name = "local-path"
        resources {
          requests = {
            storage = "5Gi"
          }
        }
      }
    }
  }
}
