variable "enable_gitea" {
  description = "Deploy Gitea and the act runner. Set to false to skip (e.g. on a dev laptop without a dedicated server)."
  type        = bool
  default     = false
}

variable "enable_monitoring" {
  description = "Deploy Prometheus, Grafana, Loki, Jaeger, and Fluent Bit. Set to false on small VMs to save ~1.5 GB RAM."
  type        = bool
  default     = true
}
