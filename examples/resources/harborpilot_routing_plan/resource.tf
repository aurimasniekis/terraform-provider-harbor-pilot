resource "harborpilot_routing_plan" "default" {
  name = "default"
  spec = jsonencode({ display_name = "Default plan" })
}
