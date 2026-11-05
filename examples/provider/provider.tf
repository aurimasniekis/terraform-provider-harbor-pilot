# Configure with environment variables (recommended):
#   export HP_SERVER=https://pilot.example.com
#   export HP_API_TOKEN_FILE=~/.config/harbor-pilot/terraform.token
provider "harborpilot" {}

# Or explicitly:
provider "harborpilot" {
  alias        = "explicit"
  url          = "https://pilot.example.com"
  token_file   = "/run/secrets/harbor-pilot-token"
  ca_cert_file = "/etc/ssl/internal-ca.pem"
  timeout      = "30s"
}
