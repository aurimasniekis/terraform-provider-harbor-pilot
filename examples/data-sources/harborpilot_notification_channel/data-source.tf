data "harborpilot_notification_channel" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_notification_channel.example.spec)
}
