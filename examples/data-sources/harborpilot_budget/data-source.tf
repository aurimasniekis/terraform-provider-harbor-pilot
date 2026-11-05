data "harborpilot_budget" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_budget.example.spec)
}
