data "harborpilot_quota_monitor" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_quota_monitor.example.spec)
}
