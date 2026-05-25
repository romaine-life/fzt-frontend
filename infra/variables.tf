variable "cluster_subscription_id" {
  description = "Azure subscription ID of the AKS cluster. Set by infra-bootstrap as the CLUSTER_SUBSCRIPTION_ID GitHub Actions variable; defaulted here so plan still works for ad-hoc tofu invocations."
  type        = string
  default     = "606a1ca1-5833-4d21-8937-d0fcd97cd0a0"
}

variable "resource_group_name" {
  description = "Resource group where the UAMI lives. Matches the shared-infra convention used by other repos in this fleet."
  type        = string
  default     = "infra"
}

variable "key_vault_name" {
  description = "FZT Frontend-owned Key Vault for app runtime secrets."
  type        = string
  default     = "ng6-fzt-frontend"
}

variable "cosmos_account_name" {
  description = "Shared Cosmos account that holds HomepageDB."
  type        = string
  default     = "infra-cosmos-serverless"
}

variable "app_configuration_name" {
  description = "Shared App Configuration store the pod's config.js reads `cosmos_db_endpoint` from."
  type        = string
  default     = "infra-appconfig"
}
