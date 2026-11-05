data "harborpilot_provider_account" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_provider_account.example.spec)
}
