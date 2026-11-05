resource "harborpilot_auth_settings" "this" {
  password_login           = true
  idle_timeout_seconds     = 1800
  absolute_timeout_seconds = 43200
}
