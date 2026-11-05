data "harborpilot_client_binding" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_client_binding.example.spec)
}
