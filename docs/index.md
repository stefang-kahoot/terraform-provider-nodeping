---
page_title: "NodePing Provider"
subcategory: ""
description: |-
  The NodePing provider allows you to manage NodePing monitoring resources including contacts and checks.
---

# NodePing Provider

The NodePing provider allows you to manage [NodePing](https://nodeping.com/) monitoring resources including contacts and checks.

## Example Usage

```terraform
terraform {
  required_providers {
    nodeping = {
      source  = "stefang-kahoot/nodeping"
      version = "~> 1.0"
    }
  }
}

provider "nodeping" {
  api_token = var.nodeping_api_token
}

resource "nodeping_contact" "email" {
  name = "DevOps Team"
  address {
    type    = "email"
    address = "devops@example.com"
  }
}

resource "nodeping_check" "website" {
  type    = "HTTP"
  target  = "https://example.com"
  label   = "Website Monitor"
  enabled = true

  notifications {
    contact_id = nodeping_contact.email.address[0].id
    delay      = 0
    schedule   = "All"
  }
}
```

## Authentication

The provider requires an API token for authentication. You can configure it in the provider block or via environment variables.

### Provider Configuration

```terraform
provider "nodeping" {
  api_token = "your-api-token"
}
```

### Environment Variables

- `NODEPING_API_TOKEN` - API token for authentication
- `NODEPING_CUSTOMER_ID` - Default SubAccount customer ID
- `NODEPING_API_URL` - API base URL (for testing)

## Multi-Account Usage

Use provider aliases to manage multiple accounts or subaccounts:

```terraform
provider "nodeping" {
  alias     = "primary"
  api_token = var.primary_token
}

provider "nodeping" {
  alias       = "subaccount"
  api_token   = var.primary_token
  customer_id = "SUBACCOUNT_ID"
}

resource "nodeping_check" "primary_check" {
  provider = nodeping.primary
  type     = "HTTP"
  target   = "https://primary.example.com"
  label    = "Primary Account Check"
}

resource "nodeping_check" "subaccount_check" {
  provider = nodeping.subaccount
  type     = "HTTP"
  target   = "https://subaccount.example.com"
  label    = "SubAccount Check"
}
```

## Default Tags

You can define default tags at the provider level that will be automatically applied to all resources that support tags (e.g., checks):

```terraform
provider "nodeping" {
  api_token    = var.nodeping_token
  default_tags = ["managed-by-terraform", "team-devops"]
}

resource "nodeping_check" "example" {
  type   = "HTTP"
  target = "https://example.com"
  label  = "Example Check"
  tags   = ["production"]
}
```

The check's `tags_all` is then `["managed-by-terraform", "team-devops", "production"]` -- the defaults first, then the check's own, with duplicates removed. That is the list NodePing receives.

`tags` itself stays exactly as written in the configuration. The two are kept apart because `tags` is the configuration's own value: merging the defaults into it would make the planned value differ from the configured one, which Terraform rejects with "Provider produced invalid plan".

On import, `tags` is reconstructed from `tags_all` by removing the provider's current `default_tags`.

## Schema

### Optional

- `api_token` (String, Sensitive) - NodePing API token. Can also be set via `NODEPING_API_TOKEN` environment variable.
- `customer_id` (String) - SubAccount customer ID for managing SubAccount resources. Can also be set via `NODEPING_CUSTOMER_ID` environment variable.
- `api_url` (String) - NodePing API base URL. Defaults to `https://api.nodeping.com/api/1`. Can also be set via `NODEPING_API_URL` environment variable.
- `rate_limit` (Number) - Maximum requests per second to the NodePing API. Defaults to `10`.
- `max_retries` (Number) - Maximum number of retries for failed requests. Defaults to `3`.
- `retry_wait_min` (Number) - Minimum wait time in seconds between retries. Defaults to `1`.
- `retry_wait_max` (Number) - Maximum wait time in seconds between retries. Defaults to `30`.
- `default_tags` (List of String) - Default tags to apply to all resources that support tags (e.g., checks). These tags are merged with resource-specific tags.
