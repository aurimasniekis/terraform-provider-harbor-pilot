data "harborpilot_provider" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_provider.example.spec)
}
