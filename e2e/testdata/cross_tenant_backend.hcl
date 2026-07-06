# Example backend configuration for a state storage account that lives in a
# different Entra tenant/subscription than the one statebridge otherwise runs
# under. Used by TestE2E_CrossTenantUploadDownload (e2e/e2e_cross_tenant_test.go)
# to prove that credential attributes supplied via --backend-config=<file>
# (client_id, tenant_id, use_oidc) are correctly merged on top of the default
# environment-sourced credential (ARM_CLIENT_ID/ARM_TENANT_ID/ARM_USE_OIDC),
# so statebridge can authenticate to this storage account via OIDC federation
# configured for this specific service principal/tenant, independent of the
# credentials used for anything else in the pipeline.
#
# storage_account_name/container_name/subscription_id/client_id/tenant_id are
# identifiers, not secrets, and (like OpenTofu's own OIDC examples) are safe
# to check in: access is governed by Azure AD federated credential trust
# (issuer + subject claim), not by the secrecy of these values.
#
# subscription_id, use_azuread_auth, and snapshot are not consumed by
# statebridge (it always authenticates via the Azure SDK using the resolved
# token credential, regardless of these azurerm-backend-only settings) and
# are included here only to mirror a real-world backend.hcl file and confirm
# they are safely ignored rather than misinterpreted.
storage_account_name = "sawftfstatesharedprd002"
container_name       = "tfstate-bandowgbr-001"
subscription_id      = "e4271ab8-fb8e-405e-bbe2-c7f27568ce8a"
client_id            = "35b1536f-c016-4dea-b3f0-fcee83c1053c"
tenant_id            = "a53834b7-42bc-46a3-b004-369735c3acf9"
use_azuread_auth     = true
use_oidc             = true
snapshot             = false
