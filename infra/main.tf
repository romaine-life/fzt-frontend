# ============================================================================
# fzt-frontend infrastructure
# ============================================================================
# Per-app workload identity for the fzt-frontend pod, plus the data-plane
# grants the pod actually needs.
#
# Migrated from romaine-life/infra-bootstrap/tofu/fzt-frontend-identity.tf as
# part of retiring the "app-specific resources in shared bootstrap" anti-
# pattern. infra-bootstrap creates the per-app SP + grants it Owner on
# the cluster sub; the app's own tofu owns everything else.
#
# Resources are imported in-place from infra-bootstrap.tfstate via the
# `import` blocks below. Companion change in infra-bootstrap drops them
# from that state with `removed { lifecycle { destroy = false } }`.
# ============================================================================

# ----------------------------------------------------------------------------
# UAMI — the pod's workload identity
# ----------------------------------------------------------------------------
resource "azurerm_user_assigned_identity" "fzt_frontend" {
  name                = "fzt-frontend-identity"
  resource_group_name = data.azurerm_resource_group.main.name
  location            = data.azurerm_resource_group.main.location
}

import {
  to = azurerm_user_assigned_identity.fzt_frontend
  id = "/subscriptions/aee0cbd2-8074-4001-b610-0f8edb4eaa3c/resourceGroups/infra/providers/Microsoft.ManagedIdentity/userAssignedIdentities/fzt-frontend-identity"
}

# ----------------------------------------------------------------------------
# Data-plane grants the pod actually uses
# ----------------------------------------------------------------------------
resource "azurerm_role_assignment" "fzt_frontend_app_keyvault" {
  scope                = azurerm_key_vault.main.id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azurerm_user_assigned_identity.fzt_frontend.principal_id
}

# ----------------------------------------------------------------------------
# Federated credential — binds the pod SA to this UAMI
# ----------------------------------------------------------------------------
# Single FIC for the dedicated-cluster topology. The pre-migration shape
# in infra-bootstrap had a paired `aks-fzt-frontend` FIC gated on the
# same-sub case (count = local.cluster_uses_dedicated_subscription ? 0
# : 1) that has never been live; only `aks-cluster-fzt-frontend` is in
# state. Carry forward just the live one.
resource "azurerm_federated_identity_credential" "fzt_frontend" {
  name                = "aks-cluster-fzt-frontend"
  resource_group_name = data.azurerm_resource_group.main.name
  parent_id           = azurerm_user_assigned_identity.fzt_frontend.id
  audience            = ["api://AzureADTokenExchange"]
  issuer              = local.aks_oidc_issuer_url
  subject             = "system:serviceaccount:fzt-frontend:infra-shared"
}

import {
  to = azurerm_federated_identity_credential.fzt_frontend
  id = "/subscriptions/aee0cbd2-8074-4001-b610-0f8edb4eaa3c/resourceGroups/infra/providers/Microsoft.ManagedIdentity/userAssignedIdentities/fzt-frontend-identity/federatedIdentityCredentials/aks-cluster-fzt-frontend"
}

output "fzt_frontend_identity_client_id" {
  value       = azurerm_user_assigned_identity.fzt_frontend.client_id
  description = "client_id of fzt-frontend-identity. Pin into fzt-frontend/k8s/serviceaccount.yaml."
}
