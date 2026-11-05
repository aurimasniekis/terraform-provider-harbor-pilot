resource "harborpilot_project" "checkout" {
  name = "checkout"
  spec = jsonencode({
    workspace_id  = harborpilot_workspace.eng.id
    display_name  = "Checkout"
    external_refs = { "jira.project" = "CHK" }
  })
}
