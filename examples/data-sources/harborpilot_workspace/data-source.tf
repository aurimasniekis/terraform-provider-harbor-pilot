data "harborpilot_workspace" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_workspace.example.spec)
}
