resource "harborpilot_workspace" "eng" {
  name = "engineering"
  spec = jsonencode({
    display_name = "Engineering"
    description  = "Product engineering teams"
  })
}
