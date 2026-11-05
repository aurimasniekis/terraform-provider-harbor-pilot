resource "harborpilot_oidc_provider" "porch" {
  slug                     = "porch"
  display_name             = "Porch"
  issuer                   = "https://porch.example.com"
  client_id                = "harbor-pilot"
  client_secret_wo         = var.oidc_client_secret
  client_secret_wo_version = 1
  scopes                   = ["openid", "profile", "email", "groups"]
  groups_claim             = "porch.groups"
  role_mappings = [
    { group = "platform-admins", role = "admin" },
    { group = "engineers", role = "operator" },
  ]
  provisioning = "jit"
  enabled      = true
}
