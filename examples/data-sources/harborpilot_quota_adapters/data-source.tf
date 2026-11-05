data "harborpilot_quota_adapters" "all" {}

output "official_adapters" {
  value = [for a in data.harborpilot_quota_adapters.all.adapters : a.id if a.tier == "official"]
}
