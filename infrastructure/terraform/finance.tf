# SESSION_SECRET must be a random 32+ byte hex string.
# Set it via: TF_VAR_finance_session_secret=<value> terraform apply
variable "finance_session_secret" {
  description = "HMAC secret for finance-api session cookies (32+ random bytes)"
  type        = string
  default     = "dev-secret-change-in-production-32x"
  sensitive   = true
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
    SESSION_SECRET        = var.finance_session_secret
    GOOGLE_CLIENT_ID      = var.finance_google_client_id
    GOOGLE_CLIENT_SECRET  = var.finance_google_client_secret
  }
}
