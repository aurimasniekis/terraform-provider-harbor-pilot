resource "harborpilot_privacy_policy" "strict" {
  name = "strict"
  spec = jsonencode({
    required = true # refuse rather than send unmasked
    rules = [
      { name = "customer-id", pattern = "CUST-[0-9]{6,}", action = "mask" },
      { name = "private-key", pattern = "-----BEGIN [A-Z ]*PRIVATE KEY-----", action = "block" },
    ]
  })
}
