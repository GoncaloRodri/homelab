variable "gitea_admin_password" {
  description = "Gitea admin password — set TF_VAR_gitea_admin_password or override in terraform.tfvars"
  type        = string
  default     = "gitea-dev-changeme"
  sensitive   = true
}

variable "gitea_runner_token" {
  description = "Gitea runner registration token — obtain from Gitea UI: Admin Area → Runners → Create Runner, then set TF_VAR_gitea_runner_token"
  type        = string
  default     = ""
  sensitive   = true
}
