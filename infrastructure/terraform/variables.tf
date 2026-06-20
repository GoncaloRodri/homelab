variable "enable_gitea" {
  description = "Deploy Gitea and the act runner. Set to false to skip (e.g. on a dev laptop without a dedicated server)."
  type        = bool
  default     = false
}
