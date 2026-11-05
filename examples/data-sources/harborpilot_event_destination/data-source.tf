data "harborpilot_event_destination" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_event_destination.example.spec)
}
