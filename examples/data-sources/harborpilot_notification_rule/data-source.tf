data "harborpilot_notification_rule" "example" {
  name = "example"
}

output "spec" {
  value = jsondecode(data.harborpilot_notification_rule.example.spec)
}
