resource "helm_release" "traefik" {
  name       = "traefik"
  namespace  = "kube-system"
  repository = "https://traefik.github.io/charts"
  chart      = "traefik"
  version    = "~> 33.0"
  atomic     = true

  values = [yamlencode({
    ports = {
      web = {
        redirectTo = { port = "websecure" }
      }
    }

    ingressRoute = {
      dashboard = { enabled = false }
    }

    additionalArguments = [
      "--certificatesresolvers.letsencrypt.acme.email=goncalo.gr@proton.me",
      "--certificatesresolvers.letsencrypt.acme.storage=/data/acme.json",
      "--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web",
    ]

    persistence = {
      enabled      = true
      size         = "128Mi"
      storageClass = "local-path"
    }

    service = {
      type = "LoadBalancer"
    }

    resources = {
      requests = { cpu = "50m", memory = "64Mi" }
      limits   = { cpu = "200m", memory = "128Mi" }
    }
  })]
}
