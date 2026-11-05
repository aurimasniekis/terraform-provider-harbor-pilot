# Pulse events and notification routing: every domain event is published to
# a Pulse (NATS JetStream) server and to a signed webhook; quota, budget and
# account-health events go to Telegram, critical ones also to Slack, with
# quiet hours at night.

variable "prefix" {
  type    = string
  default = "events"
}

variable "enable_delivery" {
  description = "Turn delivery on. Destinations and channels can be created disabled first and enabled once verified."
  type        = bool
  default     = true
}

variable "webhook_signing_secret" {
  description = "Webhook HMAC signing secret. Write-only: never stored in state."
  type        = string
  sensitive   = true
  ephemeral   = true
  default     = "change-me-signing-secret"
}

resource "harborpilot_event_destination" "pulse" {
  name = "${var.prefix}-pulse"
  spec = jsonencode({
    type         = "pulse"
    pulse_url    = "nats://pulse.example.com:4222"
    pulse_source = "harbor-pilot/prod"
    # NATS credentials file with a publish grant for evt.harborpilot.>
    creds_ref = "file:/etc/harbor-pilot/secrets/pulse.creds"
    enabled   = var.enable_delivery
  })
}

resource "harborpilot_event_destination" "audit_webhook" {
  name = "${var.prefix}-audit-webhook"
  spec = jsonencode({
    type        = "webhook"
    webhook_url = "https://audit.example.com/hooks/harbor-pilot"
    # Only configuration and access events.
    filter          = { events = ["config.*", "access.*", "policy.*"] }
    max_attempts    = 10
    timeout_seconds = 10
    enabled         = var.enable_delivery
  })
  secrets_wo         = { signing_secret = var.webhook_signing_secret }
  secrets_wo_version = 1
}

resource "harborpilot_notification_channel" "telegram" {
  name = "${var.prefix}-telegram"
  spec = jsonencode({
    type             = "telegram"
    telegram_chat_id = "-1001234567890"
    secret_ref       = "env:HP_SECRET_TELEGRAM_BOT_TOKEN"
    timezone         = "Europe/Vilnius"
    quiet_hours      = { enabled = true, start = "22:00", end = "07:00", critical_bypass = true }
    rate_per_minute  = 20
    enabled          = var.enable_delivery
  })
}

resource "harborpilot_notification_channel" "slack" {
  name = "${var.prefix}-slack"
  spec = jsonencode({
    type       = "slack"
    secret_ref = "env:HP_SECRET_SLACK_WEBHOOK_URL"
    enabled    = var.enable_delivery
  })
}

resource "harborpilot_notification_rule" "quota" {
  name = "${var.prefix}-quota-and-budgets"
  spec = jsonencode({
    channel_ids      = [harborpilot_notification_channel.telegram.id]
    filter           = { events = ["quota.*", "budget.*", "provider.account.*"], min_severity = "warning" }
    group_seconds    = 120
    cooldown_seconds = 1800
    recovery         = true
    enabled          = true
  })
}

resource "harborpilot_notification_rule" "critical" {
  name = "${var.prefix}-critical"
  spec = jsonencode({
    channel_ids = [harborpilot_notification_channel.slack.id, harborpilot_notification_channel.telegram.id]
    filter      = { min_severity = "critical" }
    recovery    = true
    enabled     = true
  })
}

data "harborpilot_event_catalog" "all" {}

output "notified_event_kinds" {
  value = [for e in data.harborpilot_event_catalog.all.events : e.name if can(regex("^(quota|budget|provider\\.account)\\.", e.name))]
}
