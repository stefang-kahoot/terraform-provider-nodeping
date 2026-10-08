# Terraform Provider for NodePing

[![Tests](https://github.com/stefang-kahoot/terraform-provider-nodeping/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/stefang-kahoot/terraform-provider-nodeping/actions/workflows/test.yml)
[![Security](https://github.com/stefang-kahoot/terraform-provider-nodeping/actions/workflows/security.yml/badge.svg?branch=main)](https://github.com/stefang-kahoot/terraform-provider-nodeping/actions/workflows/security.yml)

[![Terraform Registry](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fregistry.terraform.io%2Fv1%2Fproviders%2Fstefang-kahoot%2Fnodeping&query=%24.version&label=terraform%20registry&color=844FBA&logo=terraform)](https://registry.terraform.io/providers/stefang-kahoot/nodeping/latest)
[![Downloads](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fregistry.terraform.io%2Fv1%2Fproviders%2Fstefang-kahoot%2Fnodeping&query=%24.downloads&label=downloads&color=844FBA&logo=terraform)](https://registry.terraform.io/providers/stefang-kahoot/nodeping/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/stefang-kahoot/terraform-provider-nodeping?logo=go)](go.mod)
[![Terraform](https://img.shields.io/badge/Terraform-1.14+-purple.svg?logo=terraform)](https://www.terraform.io/)
[![License](https://img.shields.io/github/license/stefang-kahoot/terraform-provider-nodeping?color=green)](LICENSE)

A Terraform provider for managing [NodePing](https://nodeping.com/) monitoring resources.

This is a fork of [phizzl/terraform-provider-nodeping](https://github.com/phizzl/terraform-provider-nodeping),
published as [`stefang-kahoot/nodeping`](https://registry.terraform.io/providers/stefang-kahoot/nodeping/latest).
It carries fixes found while bringing an existing NodePing estate under
Terraform, mostly to how import and refresh read back what NodePing stores.
See the [releases](https://github.com/stefang-kahoot/terraform-provider-nodeping/releases)
for what changed.

## Features

- **Contacts Management**: Create, read, update, and delete NodePing contacts with multiple notification addresses
- **Contact Groups**: Bundle contact addresses into groups so a check can notify all of them through one entry
- **Checks Management**: Full CRUD support for all 30+ NodePing check types
- **Multi-Account Support**: Manage resources across primary accounts and SubAccounts using provider aliases
- **Secure Authentication**: API token via configuration or environment variables
- **Rate Limiting**: Built-in rate limiting and retry logic

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.14
- [Go](https://golang.org/doc/install) >= 1.26 (for building from source)
- A [NodePing](https://nodeping.com/) account with API access

## Installation

### From Terraform Registry (Recommended)

```hcl
terraform {
  required_providers {
    nodeping = {
      source  = "stefang-kahoot/nodeping"
      version = "~> 0.4"
    }
  }
}
```

### Building from Source

```bash
git clone https://github.com/stefang-kahoot/terraform-provider-nodeping.git
cd terraform-provider-nodeping
go build -o terraform-provider-nodeping
```

## Authentication

The provider requires a NodePing API token for authentication. You can obtain your token from the Account Settings section in the NodePing web interface.

### Option 1: Provider Configuration

```hcl
provider "nodeping" {
  api_token = var.nodeping_api_token
}
```

### Option 2: Environment Variables

```bash
export NODEPING_API_TOKEN="your-api-token"
```

### Configuration Precedence

1. Provider block configuration (highest priority)
2. Environment variables (fallback)

## Provider Configuration

```hcl
provider "nodeping" {
  # Required: API token for authentication
  # Can also be set via NODEPING_API_TOKEN environment variable
  api_token = "your-api-token"

  # Optional: SubAccount customer ID for managing SubAccount resources
  # Can also be set via NODEPING_CUSTOMER_ID environment variable
  customer_id = "201203232048C76FH"

  # Optional: API base URL (for testing)
  # Default: https://api.nodeping.com/api/1
  api_url = "https://api.nodeping.com/api/1"

  # Optional: Rate limiting (requests per second)
  # Default: 10
  rate_limit = 10

  # Optional: Retry configuration
  max_retries    = 3   # Maximum retry attempts
  retry_wait_min = 1   # Minimum wait between retries (seconds)
  retry_wait_max = 30  # Maximum wait between retries (seconds)
}
```

## Multi-Account Usage

Use provider aliases to manage resources across multiple accounts:

```hcl
# Primary account
provider "nodeping" {
  alias     = "primary"
  api_token = var.primary_token
}

# SubAccount
provider "nodeping" {
  alias       = "subaccount"
  api_token   = var.primary_token
  customer_id = "SUBACCOUNT_CUSTOMER_ID"
}

# Resources in primary account
resource "nodeping_contact" "primary_ops" {
  provider = nodeping.primary
  name     = "Primary Ops Team"
  # ...
}

# Resources in SubAccount
resource "nodeping_contact" "sub_ops" {
  provider = nodeping.subaccount
  name     = "SubAccount Ops Team"
  # ...
}
```

## Quick Start

### Create a Contact

```hcl
resource "nodeping_contact" "ops_team" {
  name     = "Operations Team"
  custrole = "notify"

  address {
    type    = "email"
    address = "ops@example.com"
  }

  address {
    type    = "sms"
    address = "+1-555-123-4567"
  }
}
```

### Create an HTTP Check

```hcl
resource "nodeping_check" "website" {
  type    = "HTTP"
  target  = "https://example.com"
  label   = "Example Website"
  enabled = true

  interval  = 5   # minutes
  threshold = 10  # seconds timeout
  sens      = 2   # rechecks before status change

  runlocations = ["nam"]  # North America
  tags         = ["production", "website"]

  notifications {
    contact_id = nodeping_contact.ops_team.id
    delay      = 0
    schedule   = "All"
  }
}
```

### Create an SSL Certificate Check

```hcl
resource "nodeping_check" "ssl_cert" {
  type        = "SSL"
  target      = "example.com"
  label       = "SSL Certificate Expiry"
  enabled     = true
  warningdays = 30
  servername  = "example.com"
}
```

### Create a Check with Notification Dependency

```hcl
# Primary check (e.g., router or core service)
resource "nodeping_check" "router" {
  type    = "PING"
  target  = "192.168.1.1"
  label   = "Edge Router"
  enabled = true
}

# Dependent check - notifications suppressed if router is down
resource "nodeping_check" "web_service" {
  type    = "HTTP"
  target  = "https://internal.example.com"
  label   = "Internal Web Service"
  enabled = true
  dep     = nodeping_check.router.id  # Suppress notifications if router check is failing
}
```

## Resources

### nodeping_contact

Manages a NodePing contact for receiving notifications.

**Example:**

```hcl
resource "nodeping_contact" "example" {
  name     = "John Doe"
  custrole = "notify"  # "edit", "view", or "notify"

  address {
    type    = "email"
    address = "john@example.com"
  }
}
```

**Attributes:**

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | No | Contact name/label |
| `custrole` | string | No | Permission role: `edit`, `view`, `notify` (default: `notify`) |
| `address` | block | No | Notification addresses (see below) |

**Address Block:**

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | Yes | Address type: `email`, `sms`, `webhook`, `slack`, `pushover`, `pagerduty`, `voice` |
| `address` | string | Yes | Address value |
| `suppress_up` | bool | No | Suppress "up" notifications |
| `suppress_down` | bool | No | Suppress "down" notifications |
| `suppress_first` | bool | No | Suppress "first result" notifications |
| `suppress_diag` | bool | No | Suppress diagnostic notifications |
| `suppress_all` | bool | No | Suppress all notifications |
| `mute` | bool | No | Mute all notifications |
| `action` | string | No | HTTP method for webhooks |
| `headers` | map | No | HTTP headers for webhooks |
| `data` | string | No | Request body for webhooks |

### nodeping_check

Manages a NodePing monitoring check.

**Supported Check Types:**

`AGENT`, `AUDIO`, `CLUSTER`, `DOHDOT`, `DNS`, `FTP`, `HTTP`, `HTTPCONTENT`, `HTTPPARSE`, `HTTPADV`, `IMAP4`, `MONGODB`, `MTR`, `MYSQL`, `NTP`, `PGSQL`, `PING`, `POP3`, `PORT`, `PUSH`, `RBL`, `RDAP`, `RDP`, `REDIS`, `SIP`, `SMTP`, `SNMP`, `SPEC10DNS`, `SPEC10RDDS`, `SSH`, `SSL`, `WEBSOCKET`, `WHOIS`

**Common Attributes:**

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | Yes | Check type |
| `target` | string | Conditional | Target URL/hostname/IP |
| `label` | string | No | Display label |
| `enabled` | bool | No | Enable the check |
| `interval` | float | No | Check interval in minutes |
| `threshold` | int | No | Timeout in seconds |
| `sens` | int | No | Rechecks before status change |
| `dep` | string | No | Check ID for notification dependency (suppresses notifications if dependent check is failing) |
| `runlocations` | list | No | Probe locations |
| `tags` | list | No | Tags for grouping |

### nodeping_contactgroup

Manages a NodePing contact group. Members are contact **address** IDs, not contact IDs.

**Example:**

```hcl
resource "nodeping_contactgroup" "escalation" {
  name = "Escalation"

  members = [
    nodeping_contact.oncall.address[0].id,
    nodeping_contact.backup.address[0].id,
  ]
}

resource "nodeping_check" "site" {
  type   = "HTTP"
  target = "https://example.com"
  label  = "Website"

  notifications {
    contact_id = nodeping_contactgroup.escalation.id
    delay      = 0
    schedule   = "All"
  }
}
```

## Data Sources

### nodeping_contact

Fetch a single contact by ID.

```hcl
data "nodeping_contact" "example" {
  id = "201205050153W2Q4C-BKPGH"
}
```

### nodeping_contacts

Fetch all contacts.

```hcl
data "nodeping_contacts" "all" {}
```

### nodeping_contactgroup

Fetch a single contact group by ID.

```hcl
data "nodeping_contactgroup" "escalation" {
  id = "201205050153W2Q4C-G-1ZIYU"
}
```

### nodeping_contactgroups

Fetch all contact groups.

```hcl
data "nodeping_contactgroups" "all" {}
```

### nodeping_check

Fetch a single check by ID, including the check-type specific parameters.
Credentials (`password`, `snmpcom`) are deliberately not exposed.

```hcl
data "nodeping_check" "example" {
  id = "201205050153W2Q4C-0J2HSIRF"
}

output "expected_content" {
  value = data.nodeping_check.example.contentstring
}
```

### nodeping_checks

Fetch all checks with optional filtering. Each entry carries the same attributes
as `nodeping_check`, including the check-type specific parameters, and the list
is ordered by ID.

```hcl
data "nodeping_checks" "http_only" {
  type = "HTTP"
}

# Filtering on a check-type specific parameter
output "following_redirects" {
  value = [
    for c in data.nodeping_checks.http_only.checks : c.label
    if c.follow == true
  ]
}
```

## Import

### Import a Contact

```bash
# Primary account
terraform import nodeping_contact.example 201205050153W2Q4C-BKPGH

# SubAccount
terraform import nodeping_contact.example CUSTOMER_ID:201205050153W2Q4C-BKPGH
```

### Import a Check

```bash
# Primary account
terraform import nodeping_check.example 201205050153W2Q4C-0J2HSIRF

# SubAccount
terraform import nodeping_check.example CUSTOMER_ID:201205050153W2Q4C-0J2HSIRF
```

### Import a Contact Group

```bash
# Primary account
terraform import nodeping_contactgroup.example 201205050153W2Q4C-G-1ZIYU

# SubAccount
terraform import nodeping_contactgroup.example CUSTOMER_ID:201205050153W2Q4C-G-1ZIYU
```

## Security Considerations

### Sensitive Data

- **API Token**: Marked as sensitive; never logged or stored in state
- **Contact Addresses**: Email addresses and phone numbers are marked as sensitive
- **Passwords**: Check passwords (FTP, SSH, etc.) are marked as sensitive
- **SNMP community strings**: `snmpcom` is treated as a credential and redacted
- **Data sources never expose credentials**: `nodeping_check` and
  `nodeping_checks` omit `password` and `snmpcom` entirely. `sshkey` and
  `clientcert` return NodePing's identifier for a stored key, not the key
  material.

### Terraform State

⚠️ **Warning**: Terraform state may contain sensitive data including:
- Contact email addresses and phone numbers
- Check target URLs and hostnames
- Webhook URLs and configurations

**Recommendations:**
- Use encrypted remote state backends (S3 with encryption, Terraform Cloud, etc.)
- Restrict access to state files
- Consider using `terraform state pull` with caution

### GDPR Compliance

This provider manages personal data (contact information). Ensure you:
- Have appropriate consent for storing contact data
- Document data processing activities
- Use `terraform destroy` to remove managed resources when no longer needed

## Development

### Building

```bash
go build -o terraform-provider-nodeping
```

### Testing

```bash
# Unit tests
go test ./...

# Acceptance tests: a real terraform binary (on PATH) against an in-process
# mock of the NodePing API. No NodePing account or API token is needed.
TF_ACC=1 go test -run TestAcc ./...

# The same, in Docker, with the Terraform version pinned in the Dockerfile
make test-acceptance
```

### Linting

```bash
make lint
```

Runs golangci-lint, at the version CI pins, with the linters configured in
`.golangci.yml`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the checks CI runs on pull requests.
Report security issues as described in [SECURITY.md](SECURITY.md).

## License

This project is licensed under the Mozilla Public License 2.0 - see the [LICENSE](LICENSE) file for details.

## Support

- [NodePing Documentation](https://nodeping.com/documentation.html)
- [NodePing API Reference](https://nodeping.com/docs-api-overview.html)
- [Issue Tracker](https://github.com/stefang-kahoot/terraform-provider-nodeping/issues)
- [Source Repository](https://github.com/stefang-kahoot/terraform-provider-nodeping)
- [Upstream](https://github.com/phizzl/terraform-provider-nodeping), which this fork is built on

## Pinned Versions

<!-- versions:start -->
<!-- Generated by scripts/sync-readme-versions.sh - do not edit by hand. -->

| Component | Version | Defined in |
|-----------|---------|------------|
| Go (toolchain used to build and test) | 1.27 | `Dockerfile`, `.github/workflows/` |
| Go (minimum required) | 1.26.0 | `go.mod` |
| Terraform CLI (acceptance tests) | 1.16.5 | `Dockerfile` |
| Alpine (runtime image) | 3.24 | `Dockerfile` |
| terraform-plugin-framework | v1.19.0 | `go.mod` |
| terraform-plugin-framework-validators | v0.19.0 | `go.mod` |
| terraform-plugin-testing | v1.16.0 | `go.mod` |
<!-- versions:end -->

The badge at the top of this file reads the Go version straight from `go.mod`,
and the table above is generated by `scripts/sync-readme-versions.sh` from
`go.mod` and the `Dockerfile`. CI runs that script with `--check`, so a
dependency bump that leaves this table stale fails the build.
