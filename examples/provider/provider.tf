terraform {
  required_providers {
    nodeping = {
      source = "registry.terraform.io/stefang-kahoot/nodeping"
    }
  }
}

# Basic provider configuration
provider "nodeping" {
  # API token can be set here or via NODEPING_API_TOKEN environment variable
  # api_token = var.nodeping_api_token
}

# Provider alias for SubAccount management
provider "nodeping" {
  alias       = "subaccount"
  customer_id = "YOUR_SUBACCOUNT_ID"
}
