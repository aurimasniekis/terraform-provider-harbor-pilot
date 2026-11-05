data "harborpilot_privacy_policy" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_privacy_policy.example.spec)
}
