resource "harborpilot_gateway_credential" "laptop" {
  binding_id = harborpilot_client_binding.claude_code.id
  name       = "laptop"
  # The token is not stored in state. Issue one with
  # `harbor-pilot token issue`, or use the harborpilot_gateway_token
  # ephemeral resource, or opt in explicitly:
  # store_token_in_state = true
}
