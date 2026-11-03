# FORGEJO-6 retirement tombstone. After the home service's live sync/write/revert
# proof, merging this removes the four AKS runtime resources from the existing
# fzt-frontend.tfstate. Apply with the operator's existing Azure CLI login; no new
# CI federation or secret is needed for infrastructure that is being removed.
terraform {
  required_version = ">= 1.11.0"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "= 4.73.0"
    }
  }
  backend "azurerm" {
    resource_group_name  = "infra"
    storage_account_name = "nelsontofu"
    container_name       = "tfstate"
    key                  = "fzt-frontend.tfstate"
    use_azuread_auth     = true
    use_cli              = true
    use_oidc             = false
  }
}

provider "azurerm" {
  features {
    key_vault {
      purge_soft_delete_on_destroy = false
    }
  }
  subscription_id                 = "aee0cbd2-8074-4001-b610-0f8edb4eaa3c"
  resource_provider_registrations = "none"
  use_cli                         = true
  use_oidc                        = false
}
