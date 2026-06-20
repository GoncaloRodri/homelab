resource "random_password" "grafana" {
  length  = 24
  special = false
}

resource "helm_release" "kube_prometheus_stack" {
  name       = "kps"
  namespace  = kubernetes_namespace.domains["monitoring"].metadata[0].name
  repository = "https://prometheus-community.github.io/helm-charts"
  chart      = "kube-prometheus-stack"
  version    = "~> 86.0"
  atomic     = true

  values = [yamlencode({
    alertmanager = {
      enabled = false
    }
    grafana = {
      adminPassword = random_password.grafana.result
      ingress = {
        enabled          = true
        hosts            = ["grafana.homelab.local"]
        ingressClassName = "traefik"
        annotations = {
          "traefik.ingress.kubernetes.io/router.middlewares" = "auth-forward-auth@kubernetescrd"
        }
      }
      additionalDataSources = [
        {
          name      = "Jaeger"
          type      = "jaeger"
          uid       = "jaeger"
          url       = "http://jaeger.monitoring.svc:16686"
          access    = "proxy"
          isDefault = false
        },
        {
          name      = "Loki"
          type      = "loki"
          uid       = "loki"
          url       = "http://loki-gateway.monitoring.svc"
          access    = "proxy"
          isDefault = false
        }
      ]
    }
    prometheus = {
      prometheusSpec = {
        resources = {
          requests = {
            cpu    = "200m"
            memory = "512Mi"
          }
          limits = {
            cpu    = "1"
            memory = "1Gi"
          }
        }
      }
    }
    kube-state-metrics = {
      resources = {
        requests = {
          cpu    = "50m"
          memory = "128Mi"
        }
      }
    }
    node-exporter = {
      resources = {
        requests = {
          cpu    = "25m"
          memory = "64Mi"
        }
      }
    }
  })]
}

resource "helm_release" "jaeger" {
  name       = "jaeger"
  namespace  = kubernetes_namespace.domains["monitoring"].metadata[0].name
  repository = "https://jaegertracing.github.io/helm-charts"
  chart      = "jaeger"
  version    = "~> 4.8"
  atomic     = true

  values = [yamlencode({
    jaeger = {
      ingress = {
        enabled = true
        hosts   = ["jaeger.homelab.local"]
        annotations = {
          "traefik.ingress.kubernetes.io/router.middlewares" = "auth-forward-auth@kubernetescrd"
        }
      }
    }
  })]
}

resource "helm_release" "loki" {
  name       = "loki"
  namespace  = kubernetes_namespace.domains["monitoring"].metadata[0].name
  repository = "https://grafana.github.io/helm-charts"
  chart      = "loki"
  version    = "~> 6.28"
  atomic     = false
  wait       = false

  values = [yamlencode({
    deploymentMode = "SingleBinary"
    loki = {
      auth_enabled = false
      commonConfig = {
        replication_factor = 1
      }
      containerSecurityContext = {
        readOnlyRootFilesystem = false
      }
      storage = {
        type = "filesystem"
        bucketNames = {
          chunks = "loki-chunks"
          ruler  = "loki-ruler"
          admin  = "loki-admin"
        }
      }
      schemaConfig = {
        configs = [{
          from         = "2024-01-01"
          store        = "tsdb"
          object_store = "filesystem"
          schema       = "v13"
          index = {
            prefix = "loki_index_"
            period = "24h"
          }
        }]
      }
    }
    singleBinary = {
      replicas    = 1
      persistence = { enabled = false }
      extraVolumes = [
        { name = "data", emptyDir = {} }
      ]
      extraVolumeMounts = [
        { name = "data", mountPath = "/var/loki" }
      ]
    }
    ruler        = { enabled = false }
    write        = { replicas = 0 }
    read         = { replicas = 0 }
    backend      = { replicas = 0 }
    chunksCache  = { enabled = false }
    resultsCache = { enabled = false }
    gateway = {
      enabled   = true
      basicAuth = { enabled = false }
      nginxConfig = {
        httpSnippet   = "proxy_set_header X-Scope-OrgID \"1\";"
        serverSnippet = ""
      }
    }
    monitoring = {
      dashboards     = { enabled = false }
      rules          = { enabled = false }
      alerts         = { enabled = false }
      serviceMonitor = { enabled = false }
      selfMonitoring = { enabled = false }
      lokiCanary     = { enabled = false }
    }
    test = { enabled = false }
  })]
}

resource "helm_release" "fluent_bit" {
  name       = "fluent-bit"
  namespace  = kubernetes_namespace.domains["monitoring"].metadata[0].name
  repository = "https://fluent.github.io/helm-charts"
  chart      = "fluent-bit"
  version    = "~> 0.48"
  atomic     = true

  values = [yamlencode({
    image = {
      repository = "docker.io/fluent/fluent-bit"
      tag        = "3.2"
    }
    config = {
      service = "[SERVICE]\n    Daemon       Off\n    Log_Level    info\n    Parsers_File /fluent-bit/etc/parsers.conf\n    HTTP_Server  On\n    HTTP_Listen  0.0.0.0\n    HTTP_Port    2020\n    Health_Check On\n"
      inputs  = "[INPUT]\n    Name             tail\n    Path             /var/log/containers/*.log\n    Exclude_Path     /var/log/containers/fluent-bit-*.log\n    multiline.parser docker,cri\n    Tag              kube.*\n    Mem_Buf_Limit    50MB\n    Skip_Long_Lines  On\n"
      filters = join("", [
        "[FILTER]\n",
        "    Name        kubernetes\n",
        "    Match       kube.*\n",
        "    Annotations Off\n",
        "    Labels      On\n",
        "\n",
        # Lift the nested 'kubernetes' object to the top level so label_keys
        # can reference flat fields like $kube_namespace_name.
        "[FILTER]\n",
        "    Name         nest\n",
        "    Match        kube.*\n",
        "    Operation    lift\n",
        "    Nested_under kubernetes\n",
        "    Add_prefix   kube_\n",
      ])
      outputs = join("", [
        "[OUTPUT]\n",
        "    Name        loki\n",
        "    Match       kube.*\n",
        "    Host        loki-gateway.monitoring.svc\n",
        "    Port        80\n",
        # Static label keeps backward compat; dynamic labels let you filter
        # {namespace="finance"} or {app="api"} in Grafana/LogQL.
        "    Labels      job=fluent-bit\n",
        "    label_keys  $kube_namespace_name,$kube_container_name\n",
      ])
    }
    tolerations = [
      { operator = "Exists" }
    ]
    daemonSetVolumes = [
      { name = "varlog", hostPath = { path = "/var/log" } },
      { name = "varlibdockercontainers", hostPath = { path = "/var/lib/docker/containers" } },
    ]
    daemonSetVolumeMounts = [
      { name = "varlog", mountPath = "/var/log" },
      { name = "varlibdockercontainers", mountPath = "/var/lib/docker/containers", readOnly = true },
    ]
  })]
}
