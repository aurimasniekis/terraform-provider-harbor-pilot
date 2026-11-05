data "harborpilot_event_catalog" "all" {}

output "critical_events" {
  value = [for e in data.harborpilot_event_catalog.all.events : e.name if e.severity == "critical"]
}
