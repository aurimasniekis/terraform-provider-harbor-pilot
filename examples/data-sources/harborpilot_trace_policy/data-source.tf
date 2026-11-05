data "harborpilot_trace_policy" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_trace_policy.example.spec)
}
