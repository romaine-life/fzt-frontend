# Shared Azure data sources. Underlying resources (RG, shared KV, AKS, Cosmos
# account, App Configuration store) live in other repos' state — this
# stack only reads them.

locals {
  shared_key_vault_name = "romaine-kv"
}

data "azurerm_client_config" "current" {}

data "azurerm_resource_group" "main" {
  name = var.resource_group_name
}

data "azurerm_key_vault" "shared" {
  name                = local.shared_key_vault_name
  resource_group_name = var.resource_group_name
}

data "azurerm_cosmosdb_account" "serverless" {
  name                = var.cosmos_account_name
  resource_group_name = var.resource_group_name
}

data "azurerm_app_configuration" "main" {
  name                = var.app_configuration_name
  resource_group_name = var.resource_group_name
}

# infra-bootstrap publishes the AKS OIDC issuer URL on its remote state.
# The UAMI's federated identity credential needs it to bind the K8s SA
# token to this UAMI.
data "terraform_remote_state" "infra_bootstrap" {
  backend = "azurerm"

  config = {
    resource_group_name  = "infra"
    storage_account_name = "nelsontofu"
    container_name       = "tfstate"
    key                  = "infra-bootstrap.tfstate"
    use_oidc             = true
  }
}

locals {
  aks_oidc_issuer_url = data.terraform_remote_state.infra_bootstrap.outputs.aks_oidc_issuer_url
}
