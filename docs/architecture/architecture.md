# Terraform Provider NodePing - Architecture

**Timestamp**: 2026-01-27T08:35:00Z  
**Meta-prompt version**: v3.8  
**Author**: Terraform Provider Engineering

---

## 1. Overview

This document describes the architecture for the Terraform Provider for NodePing, implementing CRUD operations for Contacts and Checks resources.

### Requirements Summary
- English-only codebase
- Multi-provider aliases support
- Contacts + Checks resources
- Environment variable auth fallback
- Target scale: ~50 resources per type
- GitHub repository

### Pinned Versions
| Component | Version |
|-----------|---------|
| Go | 1.25 |
| Terraform CLI | 1.14 |
| terraform-plugin-framework | latest compatible |
| terraform-plugin-testing | latest compatible |
| terraform-plugin-go | latest compatible |
| terraform-plugin-mux | latest compatible |

---

## 2. Project Structure

```
terraform-provider-nodeping/
├── .github/workflows/            # CI: test, security, release
├── .gitignore
├── .env.example                  # Example environment variables
├── CODEOWNERS
├── LICENSE
├── README.md
├── go.mod
├── go.sum
├── main.go                       # Provider entry point
├── docs/
│   ├── analysis/
│   │   └── api-analysis.md
│   ├── architecture/
│   │   └── architecture.md
│   ├── adr/                      # Architecture Decision Records
│   │   ├── 001-plugin-framework.md
│   │   └── 002-auth-handling.md
│   └── resources/
│       ├── nodeping_contact.md
│       └── nodeping_check.md
├── examples/
│   ├── provider/
│   │   └── provider.tf
│   ├── resources/
│   │   ├── nodeping_contact/
│   │   │   └── resource.tf
│   │   └── nodeping_check/
│   │       └── resource.tf
│   └── data-sources/
│       ├── nodeping_contact/
│       │   └── data-source.tf
│       └── nodeping_check/
│           └── data-source.tf
├── internal/
│   ├── provider/
│   │   ├── provider.go           # Provider implementation
│   │   └── provider_test.go
│   ├── client/
│   │   ├── client.go             # HTTP client
│   │   ├── client_test.go
│   │   ├── contacts.go           # Contacts API methods
│   │   ├── contacts_test.go
│   │   ├── checks.go             # Checks API methods
│   │   ├── checks_test.go
│   │   ├── errors.go             # Error types
│   │   └── models.go             # API data models
│   ├── resources/
│   │   ├── contact/
│   │   │   ├── resource.go
│   │   │   ├── resource_test.go
│   │   │   └── schema.go
│   │   └── check/
│   │       ├── resource.go
│   │       ├── resource_test.go
│   │       └── schema.go
│   └── datasources/
│       ├── contact/
│       │   ├── data_source.go
│       │   ├── data_source_test.go
│       │   └── schema.go
│       ├── contacts/
│       │   ├── data_source.go
│       │   └── schema.go
│       ├── check/
│       │   ├── data_source.go
│       │   ├── data_source_test.go
│       │   └── schema.go
│       └── checks/
│           ├── data_source.go
│           └── schema.go
├── testutil/
│   ├── mock_server.go            # Mock NodePing API server
│   └── fixtures/                 # Test fixtures
└── tools/
    └── tools.go                  # Tool dependencies
```

---

## 3. Provider Configuration

### Schema

```hcl
provider "nodeping" {
  api_token   = "your-api-token"     # Optional, falls back to env
  customer_id = "subaccount-id"      # Optional, for SubAccount operations
  api_url     = "https://api.nodeping.com/api/1"  # Optional, for testing
  
  # Rate limiting
  rate_limit  = 10                   # Optional, requests per second
  
  # Retry configuration
  max_retries     = 3                # Optional
  retry_wait_min  = 1                # Optional, seconds
  retry_wait_max  = 30               # Optional, seconds
}
```

### Environment Variables

| Variable | Description | Priority |
|----------|-------------|----------|
| `NODEPING_API_TOKEN` | API authentication token | Fallback if not in config |
| `NODEPING_CUSTOMER_ID` | Default SubAccount ID | Fallback if not in config |
| `NODEPING_API_URL` | API base URL (testing) | Fallback if not in config |

### Multi-Account (Alias) Pattern

```hcl
provider "nodeping" {
  alias     = "primary"
  api_token = var.primary_token
}

provider "nodeping" {
  alias       = "subaccount"
  api_token   = var.primary_token
  customer_id = "201203232048C76FH"
}

resource "nodeping_contact" "primary_contact" {
  provider = nodeping.primary
  name     = "Primary Contact"
  # ...
}

resource "nodeping_contact" "sub_contact" {
  provider = nodeping.subaccount
  name     = "SubAccount Contact"
  # ...
}
```

---

## 4. HTTP Client Design

### Client Structure

```go
type Client struct {
    httpClient  *http.Client
    baseURL     string
    apiToken    string
    customerID  string
    rateLimiter *rate.Limiter
    maxRetries  int
    retryWait   RetryConfig
    userAgent   string
}

type RetryConfig struct {
    MinWait time.Duration
    MaxWait time.Duration
}
```

### Authentication

- Uses HTTP Basic Auth (token as username, empty password)
- Token marked as `Sensitive` in provider schema
- Never logged or included in error messages

### Request Flow

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐     ┌──────────────┐
│   Request   │────▶│ Rate Limiter │────▶│   Retry     │────▶│   Execute    │
│   Method    │     │   (wait)     │     │   Logic     │     │   HTTP       │
└─────────────┘     └──────────────┘     └─────────────┘     └──────────────┘
                                                │
                                                ▼
                                         ┌──────────────┐
                                         │   Response   │
                                         │   Handler    │
                                         └──────────────┘
```

### Retry Strategy

- Retry on: 429 (rate limit), 500, 502, 503, 504
- No retry on: 400, 401, 403, 404
- Exponential backoff with jitter
- Configurable max retries (default: 3)

### Error Handling

```go
type APIError struct {
    StatusCode int
    Message    string
    RequestID  string  // If available from headers
}

func (e *APIError) Error() string {
    return fmt.Sprintf("NodePing API error (status %d): %s", e.StatusCode, e.Message)
}

type NotFoundError struct {
    ResourceType string
    ResourceID   string
}

type ValidationError struct {
    Field   string
    Message string
}
```

### Logging

- Structured logging with UTC timestamps
- Log levels: DEBUG, INFO, WARN, ERROR
- Never log tokens or sensitive data
- Log request method, URL (without token), status code, duration

---

## 5. Resource Schemas

### nodeping_contact

```hcl
resource "nodeping_contact" "example" {
  name      = "John Doe"
  custrole  = "notify"  # "edit", "view", "notify"
  
  address {
    type    = "email"
    address = "john@example.com"
    
    # Notification suppression
    suppress_up    = false
    suppress_down  = false
    suppress_first = false
    suppress_diag  = false
    suppress_all   = false
    mute           = false
  }
  
  address {
    type    = "webhook"
    address = "https://hooks.example.com/notify"
    
    # Webhook-specific
    action       = "post"
    headers      = { "Content-Type" = "application/json" }
    querystrings = { "key" = "value" }
    data         = jsonencode({ "event" = "nodeping" })
  }
  
  address {
    type     = "pushover"
    address  = "user-key"
    priority = 1
  }
}
```

#### Attributes

| Attribute | Type | Required | Computed | Sensitive | Description |
|-----------|------|----------|----------|-----------|-------------|
| `id` | string | - | yes | no | Contact ID |
| `customer_id` | string | - | yes | no | Account ID |
| `name` | string | no | no | no | Contact name |
| `custrole` | string | no | no | no | Permission role |
| `address` | block list | no | no | - | Contact addresses |

#### Address Block Attributes

| Attribute | Type | Required | Sensitive | Description |
|-----------|------|----------|-----------|-------------|
| `id` | string | computed | no | Address ID |
| `type` | string | yes | no | Address type |
| `address` | string | yes | yes* | Address value |
| `suppress_up` | bool | no | no | Suppress up notifications |
| `suppress_down` | bool | no | no | Suppress down notifications |
| `suppress_first` | bool | no | no | Suppress first notifications |
| `suppress_diag` | bool | no | no | Suppress diagnostic notifications |
| `suppress_all` | bool | no | no | Suppress all notifications |
| `mute` | bool | no | no | Mute all notifications |
| `action` | string | no | no | Webhook HTTP method |
| `headers` | map | no | no | Webhook headers |
| `querystrings` | map | no | no | Webhook query strings |
| `data` | string | no | no | Webhook body |
| `priority` | int | no | no | Pushover priority |

*`address` is sensitive for types containing personal data (email, phone)

### nodeping_check

```hcl
resource "nodeping_check" "http_example" {
  type    = "HTTP"
  target  = "https://example.com"
  label   = "Example Website"
  enabled = true
  
  interval  = 5      # minutes
  threshold = 10     # seconds timeout
  sens      = 2      # rechecks
  
  runlocations = ["nam"]  # or ["ca", "ny", "tx"]
  
  notifications {
    contact_id = nodeping_contact.example.addresses["email"].id
    delay      = 0
    schedule   = "All"
  }
  
  tags = ["production", "critical"]
}

resource "nodeping_check" "dns_example" {
  type          = "DNS"
  target        = "8.8.8.8"
  label         = "DNS Check"
  enabled       = true
  
  dns_type       = "A"
  dns_to_resolve = "example.com"
  contentstring  = "93.184.216.34"
}

resource "nodeping_check" "ssl_example" {
  type         = "SSL"
  target       = "example.com"
  label        = "SSL Certificate"
  enabled      = true
  
  warningdays  = 30
  servername   = "example.com"
}
```

#### Common Attributes

| Attribute | Type | Required | Computed | Sensitive | Description |
|-----------|------|----------|----------|-----------|-------------|
| `id` | string | - | yes | no | Check ID |
| `customer_id` | string | - | yes | no | Account ID |
| `type` | string | yes | no | no | Check type |
| `target` | string | conditional | no | no | Check target |
| `label` | string | no | no | no | Display label |
| `enabled` | bool | no | no | no | Check enabled |
| `public` | bool | no | no | no | Public reports |
| `interval` | number | no | no | no | Check interval (minutes) |
| `threshold` | int | no | no | no | Timeout (seconds) |
| `sens` | int | no | no | no | Sensitivity (rechecks) |
| `runlocations` | list(string) | no | no | no | Probe locations |
| `homeloc` | string | no | no | no | Home location |
| `dep` | string | no | no | no | Dependency check ID |
| `mute` | bool | no | no | no | Mute notifications |
| `description` | string | no | no | no | Description |
| `tags` | list(string) | no | no | no | Tags |
| `notifications` | block list | no | no | no | Notification config |
| `state` | int | - | yes | no | Current state (0/1) |
| `created` | int | - | yes | no | Creation timestamp |
| `modified` | int | - | yes | no | Modification timestamp |

#### Type-Specific Attributes (selected)

| Attribute | Types | Description |
|-----------|-------|-------------|
| `contentstring` | DNS, DOHDOT, FTP, HTTPCONTENT, HTTPADV, SSH, WEBSOCKET, WHOIS | Match string |
| `regex` | HTTPCONTENT, HTTPADV | Treat contentstring as regex |
| `follow` | HTTP, HTTPCONTENT, HTTPADV | Follow redirects |
| `method` | HTTPADV, DOHDOT | HTTP method |
| `statuscode` | HTTPADV, DOHDOT | Expected status code |
| `sendheaders` | HTTPADV, HTTPPARSE, DOHDOT | Request headers |
| `port` | DNS, FTP, NTP, PORT, SSH, IMAP4, POP3, SMTP | Port number |
| `username` | FTP, IMAP4, MYSQL, POP3, SMTP, SSH | Auth username |
| `password` | FTP, IMAP4, MYSQL, POP3, SMTP, SSH | Auth password (sensitive) |
| `dns_type` | DNS, DOHDOT | DNS query type |
| `dns_to_resolve` | DNS, DOHDOT | FQDN to resolve |
| `warningdays` | IMAP4, POP3, RDAP, SMTP, SSL, WHOIS | Days before expiry |
| `verify` | SMTP, IMAP4, POP3, DOHDOT, PGSQL, DNS | Verify SSL/DNSSEC |
| `ipv6` | HTTP, HTTPCONTENT, HTTPADV, MYSQL, PING, PORT, RDAP, WHOIS, DOHDOT, MTR | Use IPv6 |

---

## 6. Data Source Schemas

### nodeping_contact (singular)

```hcl
data "nodeping_contact" "example" {
  id = "201205050153W2Q4C-BKPGH"
}

output "contact_name" {
  value = data.nodeping_contact.example.name
}
```

### nodeping_contacts (plural)

```hcl
data "nodeping_contacts" "all" {}

output "contact_ids" {
  value = [for c in data.nodeping_contacts.all.contacts : c.id]
}
```

### nodeping_check (singular)

```hcl
data "nodeping_check" "example" {
  id = "201205050153W2Q4C-0J2HSIRF"
}
```

### nodeping_checks (plural)

```hcl
data "nodeping_checks" "all" {
  # Optional filters
  type = "HTTP"
  tags = ["production"]
}
```

---

## 7. State Model

### Contact State

```json
{
  "id": "201205050153W2Q4C-BKPGH",
  "customer_id": "201205050153W2Q4C",
  "name": "John Doe",
  "custrole": "notify",
  "address": [
    {
      "id": "K5SP9CQP",
      "type": "email",
      "address": "john@example.com",
      "suppress_up": false,
      "suppress_down": false,
      "suppress_first": false,
      "suppress_diag": false,
      "suppress_all": false,
      "mute": false
    }
  ]
}
```

### Check State

```json
{
  "id": "201205050153W2Q4C-0J2HSIRF",
  "customer_id": "201205050153W2Q4C",
  "type": "HTTP",
  "target": "https://example.com",
  "label": "Example Website",
  "enabled": true,
  "interval": 5,
  "threshold": 10,
  "sens": 2,
  "runlocations": ["nam"],
  "notifications": [
    {
      "contact_id": "K5SP9CQP",
      "delay": 0,
      "schedule": "All"
    }
  ],
  "state": 1,
  "created": 1336185808566,
  "modified": 1336759793520
}
```

---

## 8. Import Strategy

Every resource is imported by its plain NodePing ID. There is no account
prefix: the import ID format is `{resource_id}` and nothing else.

```bash
terraform import nodeping_contact.example      201205050153W2Q4C-BKPGH
terraform import nodeping_check.example        201205050153W2Q4C-0J2HSIRF
terraform import nodeping_contactgroup.example 201205050153W2Q4C-G-1ZIYU
```

### SubAccounts

A SubAccount resource is imported through a provider instance configured for
that account:

```hcl
provider "nodeping" {
  alias       = "subaccount"
  api_token   = var.nodeping_token
  customer_id = "SUBACCOUNT_ID"
}

resource "nodeping_check" "example" {
  provider = nodeping.subaccount
  # ...
}
```

The account therefore lives on the client, which applies it to every request,
rather than on the import ID. A `{customer_id}:{resource_id}` form used to be
accepted and is not any more: it scoped the import's own GET and nothing else,
so the Read that follows an import addressed the base account, 404'd, and left
Terraform proposing to recreate the resource in the wrong place. `customer_id`
is Computed on all three resources, so there was nowhere to pin the account
either. `internal/importid` rejects the form with a message naming the alias to
write instead.

---

## 9. Concurrency & Thread Safety

- HTTP client is goroutine-safe
- Rate limiter shared across all requests
- No shared mutable state in resources
- Each resource operation is atomic

---

## 10. Security Controls

### Authentication
- Token stored in provider config (marked Sensitive)
- Environment variable fallback
- HTTP Basic Auth for API calls
- No token in logs or error messages

### Data Protection
- Sensitive fields marked in schema
- State encryption recommended in docs
- Personal data minimization (only store what's needed)

### Supply Chain
- All dependencies pinned in go.mod
- SBOM generation in CI
- gosec scanning for vulnerabilities

---

## 11. Diagrams

### Component Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                     Terraform Provider                          │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │  Provider   │  │  Resources  │  │     Data Sources        │  │
│  │  Config     │  │             │  │                         │  │
│  │             │  │ - contact   │  │ - contact / contacts    │  │
│  │ - api_token │  │ - check     │  │ - check / checks        │  │
│  │ - customer  │  │             │  │                         │  │
│  └──────┬──────┘  └──────┬──────┘  └───────────┬─────────────┘  │
│         │                │                     │                │
│         └────────────────┼─────────────────────┘                │
│                          │                                      │
│                   ┌──────▼──────┐                               │
│                   │   Client    │                               │
│                   │             │                               │
│                   │ - Auth      │                               │
│                   │ - Retry     │                               │
│                   │ - Rate Limit│                               │
│                   └──────┬──────┘                               │
└──────────────────────────┼──────────────────────────────────────┘
                           │
                           ▼
                 ┌─────────────────┐
                 │  NodePing API   │
                 │                 │
                 │ /api/1/contacts │
                 │ /api/1/checks   │
                 └─────────────────┘
```

### Request Lifecycle

```
┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
│ Terraform│    │ Provider │    │  Client  │    │ NodePing │
│   Core   │    │          │    │          │    │   API    │
└────┬─────┘    └────┬─────┘    └────┬─────┘    └────┬─────┘
     │               │               │               │
     │  Create       │               │               │
     │──────────────▶│               │               │
     │               │  POST contact │               │
     │               │──────────────▶│               │
     │               │               │  HTTP POST    │
     │               │               │──────────────▶│
     │               │               │               │
     │               │               │  201 Created  │
     │               │               │◀──────────────│
     │               │  Contact obj  │               │
     │               │◀──────────────│               │
     │  State update │               │               │
     │◀──────────────│               │               │
     │               │               │               │
```

---

*Document generated as part of Phase 2: Architecture*
