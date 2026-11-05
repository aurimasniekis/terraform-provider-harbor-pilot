resource "harborpilot_notification_rule" "quota" {
  name = "quota-alerts"
  spec = jsonencode({
    channel_ids      = [harborpilot_notification_channel.telegram.id]
    filter           = { events = ["quota.*", "budget.*"], min_severity = "warning" }
    cooldown_seconds = 1800
    recovery         = true
    enabled          = true
  })
}
