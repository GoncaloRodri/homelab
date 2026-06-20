# Session secret is auto-generated and stored in Terraform state (gitignored).
# To rotate: terraform taint random_password.finance_session_secret && terraform apply
resource "random_password" "finance_session_secret" {
  length  = 48
  special = false # alphanumeric only — safe as an env var value
}

variable "finance_google_client_id" {
  description = "Google OAuth client ID for finance-api (optional)"
  type        = string
  default     = ""
  sensitive   = false
}

variable "finance_google_client_secret" {
  description = "Google OAuth client secret for finance-api (optional)"
  type        = string
  default     = ""
  sensitive   = true
}

resource "kubernetes_secret" "finance_api" {
  metadata {
    name      = "finance-api-secrets"
    namespace = kubernetes_namespace.domains["finance"].metadata[0].name
  }
  data = {
    SESSION_SECRET       = random_password.finance_session_secret.result
    GOOGLE_CLIENT_ID     = var.finance_google_client_id
    GOOGLE_CLIENT_SECRET = var.finance_google_client_secret
  }
}
