data "harborpilot_routing_plan" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_routing_plan.example.spec)
}
