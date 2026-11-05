data "harborpilot_pool" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_pool.example.spec)
}
