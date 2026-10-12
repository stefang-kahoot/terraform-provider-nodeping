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

Every setting must be known when Terraform plans. One taken from a resource that is still to be created, such as `api_token = terraform_data.token.output`, fails the plan, and its environment variable does not stand in for it, since the configured value overrides it. Apply that resource first, for example with `-target`.

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

## NodePing plans

Some NodePing features come with some plans only. Set `plan` to the account's plan when a configuration uses one of them:

```terraform
provider "nodeping" {
  api_token = var.nodeping_token
  plan      = "premiere" # "personal", "professional" or "premiere"
}
```

Leaving `plan` out stands for an older or unknown plan, such as a grandfathered Business plan, and allows none of these features. A configuration that uses a feature the declared plan lacks fails the plan. The error names the setting and the plans that have the feature.

| Feature | Settings | Plans that have it |
|---|---|---|
| Specify/roaming location | check `homeloc`, any value | `premiere` |
| AUDIO volume detection | check `verifyvolume` and `volumemin`, any value | `premiere` |
| Voice notifications | contact address `type = "voice"` | `premiere` |
| Sub-minute intervals | check `interval` of `0.25` or `0.5` | `professional`, `premiere` |
| Automated Diagnostics | check `autodiag = true`; contact address `suppress_diag = true` | `personal`, `professional`, `premiere` |

`false` for `autodiag` and `suppress_diag` is always allowed: NodePing holds it when neither is in use. The table follows NodePing's [pricing page](https://nodeping.com/pricing) and API documentation, checked in October 2026.

- The provider is tested on a grandfathered Business plan. There NodePing drops all of these settings without an error, or refuses them: `voice` addresses and sub-minute intervals.
- The behaviour on the Personal, Professional and Premiere plans, `autodiag` and `suppress_diag` included, is untested: no such account was available. Testing on them is tracked in [#39](https://github.com/stefang-kahoot/terraform-provider-nodeping/issues/39).

## Schema

### Optional

- `api_token` (String, Sensitive) - NodePing API token. Can also be set via `NODEPING_API_TOKEN` environment variable.
- `customer_id` (String) - SubAccount customer ID for managing SubAccount resources. Can also be set via `NODEPING_CUSTOMER_ID` environment variable.
- `api_url` (String) - NodePing API base URL. Defaults to `https://api.nodeping.com/api/1`. Can also be set via `NODEPING_API_URL` environment variable.
- `rate_limit` (Number) - Maximum requests per second to the NodePing API, greater than `0`. Defaults to `10`.
- `max_retries` (Number) - Maximum number of retries for a failed request: one answered 429 or with a server error, or whose connection failed. A request that creates something is retried only if it cannot have reached NodePing. `0` turns retries off. Defaults to `3`.
- `retry_wait_min` (Number) - Minimum wait time in seconds between retries. `0` sends retries without waiting, unless a 429 asks for a wait in `Retry-After`. Defaults to `1`.
- `retry_wait_max` (Number) - Maximum wait time in seconds between retries, also for a 429 that asks for a longer wait in `Retry-After`. `0` sends retries without waiting, even after such a 429. Defaults to `30`.
- `default_tags` (List of String) - Default tags to apply to all resources that support tags (e.g., checks). These tags are merged with resource-specific tags.
- `ignore_mute` (Boolean) - Leave `mute` to NodePing on every check and contact address whose configuration does not set it: a plan never changes it and an update does not send it, so a check or address muted or unmuted in the NodePing web interface stays that way. A check or address that sets `mute` is still managed. Defaults to `false`. See [Muting in NodePing](#muting-in-nodeping).
- `plan` (String) - The account's NodePing plan: `personal`, `professional` or `premiere`. Leave it out for an older or unknown plan, such as a grandfathered Business plan. A setting that uses a feature the plan lacks fails the plan: `homeloc`, `verifyvolume`, `volumemin` and a `voice` contact address need `premiere`; an `interval` of `0.25` or `0.5` needs `professional` or `premiere`; `autodiag = true` and `suppress_diag = true` need any of the three. See [NodePing plans](#nodeping-plans).
