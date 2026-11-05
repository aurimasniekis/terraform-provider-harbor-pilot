data "harborpilot_subscription_entitlement" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_subscription_entitlement.example.spec)
}
