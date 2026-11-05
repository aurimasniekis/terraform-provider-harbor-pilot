terraform {
  required_version = ">= 1.11"
  required_providers {
    harborpilot = {
      source = "aurimasniekis/harbor-pilot"
    }
  }
}

# Reads HP_SERVER and HP_API_TOKEN (or HP_API_TOKEN_FILE).
provider "harborpilot" {}
