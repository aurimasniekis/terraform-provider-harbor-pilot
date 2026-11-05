resource "harborpilot_notification_channel" "telegram" {
  name = "ops-telegram"
  spec = jsonencode({
    type             = "telegram"
    telegram_chat_id = "-1001234567890"
    secret_ref       = "env:TELEGRAM_BOT_TOKEN"
    quiet_hours      = { enabled = true, start = "22:00", end = "07:00", critical_bypass = true }
    timezone         = "Europe/Vilnius"
    enabled          = true
  })
}
