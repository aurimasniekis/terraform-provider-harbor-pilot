data "harborpilot_project" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_project.example.spec)
}
