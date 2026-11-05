resource "harborpilot_model_alias" "coding" {
  name = "coding/default"
  spec = jsonencode({
    pool_id               = harborpilot_pool.coding.id
    required_capabilities = ["tools"]
  })
}
