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
      version = "~> 0.4"
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

A resource is imported through the provider it names, so a SubAccount's
resources are imported by their plain ID through the SubAccount's provider.

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

## Muting in NodePing

Set `ignore_mute` when checks and contact addresses are muted and unmuted in the NodePing web interface rather than in Terraform:

```terraform
provider "nodeping" {
  api_token   = var.nodeping_token
  ignore_mute = true
}
```

Terraform then leaves `mute` alone on every check, and every contact `address` block, whose configuration does not set it, with no `lifecycle { ignore_changes = [...] }` needed:

- A plan never changes `mute`. It keeps the value the last refresh read from NodePing, `true` for a check or address muted until a set time as well.
- An update does not send `mute`, so NodePing keeps whatever it holds, including a mute set after the plan was made, as when a saved plan is applied later. The state keeps the planned value until the next refresh reads the real one.
- A new check or address starts unmuted.

A check or address that sets `mute` itself is still managed, the same as without `ignore_mute`.

## Schema

### Optional

- `api_token` (String, Sensitive) - NodePing API token. Can also be set via `NODEPING_API_TOKEN` environment variable.
- `customer_id` (String) - SubAccount customer ID for managing SubAccount resources. Can also be set via `NODEPING_CUSTOMER_ID` environment variable.
- `api_url` (String) - NodePing API base URL. Defaults to `https://api.nodeping.com/api/1`. Can also be set via `NODEPING_API_URL` environment variable.
- `rate_limit` (Number) - Maximum requests per second to the NodePing API. Defaults to `10`.
- `max_retries` (Number) - Maximum number of retries for a failed request: one answered 429 or with a server error, or whose connection failed. A request that creates something is retried only if it cannot have reached NodePing. Defaults to `3`.
- `retry_wait_min` (Number) - Minimum wait time in seconds between retries. Defaults to `1`.
- `retry_wait_max` (Number) - Maximum wait time in seconds between retries, also for a 429 that asks for a longer wait in `Retry-After`. Defaults to `30`.
- `default_tags` (List of String) - Default tags to apply to all resources that support tags (e.g., checks). These tags are merged with resource-specific tags.
- `ignore_mute` (Boolean) - Leave `mute` to NodePing on every check and contact address whose configuration does not set it: a plan never changes it and an update does not send it, so a check or address muted or unmuted in the NodePing web interface stays that way. A check or address that sets `mute` is still managed. Defaults to `false`. See [Muting in NodePing](#muting-in-nodeping).
