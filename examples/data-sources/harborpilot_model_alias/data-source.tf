data "harborpilot_model_alias" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_model_alias.example.spec)
}
