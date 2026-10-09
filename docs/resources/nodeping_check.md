---
page_title: "nodeping_check Resource - terraform-provider-nodeping"
subcategory: ""
description: |-
  Manages a NodePing monitoring check.
---

# nodeping_check (Resource)

Manages a NodePing monitoring check.

## Example Usage

### HTTP Check

```hcl
resource "nodeping_check" "http" {
  type    = "HTTP"
  target  = "https://example.com"
  label   = "Example Website"
  enabled = true

  interval  = 5
  threshold = 10
  sens      = 2

  runlocations = ["nam"]
  tags         = ["production", "website"]
}
```

### HTTPS Content Check

```hcl
resource "nodeping_check" "content" {
  type          = "HTTPCONTENT"
  target        = "https://api.example.com/health"
  label         = "API Health Check"
  enabled       = true
  contentstring = "\"status\":\"ok\""
  regex         = false
}
```

### HTTP Parse Check

```hcl
resource "nodeping_check" "stats" {
  type    = "HTTPPARSE"
  target  = "https://api.example.com/stats.json"
  label   = "API Stats"
  enabled = true

  fields = {
    A = { name = "status", min = 200, max = 200 }
    B = { name = "queue.length", max = 1000 }
  }
}
```

### DNS Check

```hcl
resource "nodeping_check" "dns" {
  type          = "DNS"
  target        = "8.8.8.8"
  label         = "DNS Resolution"
  enabled       = true
  dnstype       = "A"
  dnstoresolve  = "example.com"
  contentstring = "93.184.216.34"
}
```

### SSL Certificate Check

```hcl
resource "nodeping_check" "ssl" {
  type        = "SSL"
  target      = "example.com"
  label       = "SSL Certificate Expiry"
  enabled     = true
  warningdays = 30
  servername  = "example.com"
}
```

### PING Check

```hcl
resource "nodeping_check" "ping" {
  type    = "PING"
  target  = "8.8.8.8"
  label   = "Google DNS Ping"
  enabled = true
}
```

### PORT Check

```hcl
resource "nodeping_check" "port" {
  type    = "PORT"
  target  = "example.com"
  label   = "SSH Port"
  enabled = true
  port    = 22
}
```

### SMTP Check with TLS

```hcl
resource "nodeping_check" "smtp" {
  type        = "SMTP"
  target      = "mail.example.com"
  label       = "Mail Server"
  enabled     = true
  port        = 587
  secure      = "starttls"
  warningdays = 14
}
```

### Audio Stream Check

```hcl
resource "nodeping_check" "audio" {
  type    = "AUDIO"
  target  = "https://example.com/stream.mp3"
  label   = "Audio Stream"
  enabled = true

  verifyvolume = true
  volumemin    = -40
}
```

### Check with Notifications

```hcl
resource "nodeping_check" "with_notifications" {
  type    = "HTTP"
  target  = "https://critical.example.com"
  label   = "Critical Service"
  enabled = true

  interval  = 1
  threshold = 5
  sens      = 1

  notifications {
    contact_id = nodeping_contact.ops.id
    delay      = 0
    schedule   = "All"
  }

  notifications {
    contact_id = nodeping_contact.escalation.id
    delay      = 15
    schedule   = "All"
  }
}
```

### Check with Dependency

```hcl
resource "nodeping_check" "router" {
  type    = "PING"
  target  = "192.168.1.1"
  label   = "Edge Router"
  enabled = true
}

resource "nodeping_check" "service" {
  type    = "HTTP"
  target  = "https://internal.example.com"
  label   = "Internal Service"
  enabled = true
  dep     = nodeping_check.router.id
}
```

## Argument Reference

### Common Arguments

- `type` - (Required) The type of check. See [Supported Check Types](#supported-check-types).
- `target` - (Required for most types) The target URL, hostname, or IP address.
- `label` - (Optional) Display label for the check. Defaults to target.
- `enabled` - (Optional) Whether the check is enabled. Defaults to `false`.
- `public` - (Optional) Enable public reports. Defaults to `false`.
- `interval` - (Optional) Check interval in minutes. Can be `0.25`, `0.5`, or any integer >= 1. Defaults to `15`.
- `threshold` - (Optional) Timeout in seconds. Defaults to `5`.
- `sens` - (Optional) Number of rechecks before status change. Defaults to `2`.
- `mute` - (Optional) Mute all notifications. Defaults to `false`. With the provider's `ignore_mute` set, a check that leaves `mute` out has its mute left to NodePing: Terraform neither plans nor sends it, so a mute set in the NodePing web interface stays.
- `dep` - (Optional) Check ID for notification dependency.
- `description` - (Optional) Description text (max 1000 characters).
- `autodiag` - (Optional) Enable automated diagnostics. Defaults to `false`.

### Location Arguments

- `runlocations` - (Optional) List of probe locations. Can be region codes (`nam`, `lam`, `eur`, `eao`, `wlw`) or probe codes (`ca`, `ny`, `tx`, etc.).
- `homeloc` - (Optional) Preferred probe location or `roam` for rotating.

### Tagging

- `tags` - (Optional) List of tags for grouping checks, as written in the
  configuration. The provider's `default_tags` are not included here.

### Attribute Reference (tags)

- `tags_all` - Every tag applied to the check: `tags` merged with the
  provider's `default_tags`, defaults first, deduplicated. This is what the
  check actually carries in NodePing.

### Notifications Block

- `contact_id` - (Required) Contact or contact group ID to notify.
- `delay` - (Optional) Delay in minutes before sending notification. Defaults to `0`.
- `schedule` - (Optional) Notification schedule name.

### Content Matching Arguments

- `contentstring` - (Optional) String to match in response.
- `regex` - (Optional) Treat contentstring as regular expression.
- `invert` - (Optional) Invert match (does not contain).

### HTTP Arguments

- `follow` - (Optional) Follow redirects (up to 4).
- `method` - (Optional) HTTP method for HTTPADV: `GET`, `POST`, `PUT`, `HEAD`, `TRACE`, `CONNECT`.
- `statuscode` - (Optional) Expected HTTP status code.
- `sendheaders` - (Optional) Map of request headers.
- `receiveheaders` - (Optional) Map of expected response headers.
- `postdata` - (Optional) POST request body.
- `ipv6` - (Optional) Use IPv6.

### Parse Arguments

- `fields` - (Optional) Values to parse out of the response, as a map keyed by
  NodePing's key for each field. The key can be any string: NodePing only uses
  it to tell fields apart, and its web interface makes up a random one. Import
  keeps the keys a check already has, so a configuration has to use the same
  ones. `HTTPPARSE`, `SNMP`, `MYSQL`, `PGSQL` and `MONGODB` checks. Each entry
  takes:
  - `name` - (Required) Name or path of the value, e.g. `status` or
    `content.400.defaultOutput`.
  - `min` - (Optional) Lowest acceptable value.
  - `max` - (Optional) Highest acceptable value.
  - `match` - (Optional) String the value has to match. `MYSQL`, `PGSQL` and
    `MONGODB` only.

  Changing a field's values or adding a field updates the check in place.
  Removing a field, or a field's `min`, `max` or `match`, replaces the check:
  NodePing cannot remove them from an existing check (an update keeps them),
  so Terraform deletes it and creates a new one with a new ID, and the plan
  says so in a warning. Put them back in the configuration to keep the check.

### DNS Arguments

- `dnstype` - (Optional) DNS query type: `ANY`, `A`, `AAAA`, `CNAME`, `MX`, `NS`, `PTR`, `SOA`, `SRV`, `TXT`.
- `dnstoresolve` - (Optional) FQDN to resolve.
- `dnssection` - (Optional) DNS section to check: `answer`, `authority`, `additional`, `edns_options`.
- `dnsrd` - (Optional) Recursion Desired bit. Defaults to `true`.
- `transport` - (Optional) Transport protocol: `udp`, `tcp`.

### SSL/TLS Arguments

- `warningdays` - (Optional) Days before expiry to fail check. Leave unset for the check to fail only once the certificate expires. NodePing's web interface stores that as `0`, which reads back as unset.
- `servername` - (Optional) Server name for SNI.
- `verify` - (Optional) Verify SSL certificate.
- `secure` - (Optional) SSL mode: `false`, `ssl`, `starttls`.

### Authentication Arguments

- `username` - (Optional) Authentication username.
- `password` - (Optional, Sensitive) Authentication password.
- `sshkey` - (Optional) SSH private key ID.
- `clientcert` - (Optional) Client certificate ID.

### Network Arguments

- `port` - (Optional/Required) Port number. Required for PORT and NTP checks.

### Database Arguments

- `database` - (Optional) Database name.
- `query` - (Optional) Query to execute.
- `namespace` - (Optional) MongoDB collection namespace.

### SNMP Arguments

- `snmpv` - (Optional) SNMP version: `1`, `2c`.
- `snmpcom` - (Optional) SNMP community string.

### Audio Arguments

- `verifyvolume` - (Optional) Enable the volume detection feature. `AUDIO` checks only.
- `volumemin` - (Optional) Minimum acceptable volume threshold in dB, used by the volume detection feature. Range: `-90` to `0`. `AUDIO` checks only.

## Removing an Argument

NodePing keeps whatever an update leaves out, so removing an argument from the
configuration has to clear it in NodePing explicitly. The provider sends the
value NodePing stores as cleared, and only for a value the check has:

- `contentstring`, `method`, `postdata`, `servername`, `statuscode` and
  `warningdays` are cleared to an empty value.
- `regex`, `invert`, `follow` and `ipv6` are set to `false`. Once set, NodePing
  keeps these as `false` rather than dropping them. A refresh reads a stored
  `false` as unset when the configuration leaves the argument out. An import
  has no configuration to go by and reads it as `false`, so a configuration
  that imports such a check without a plan says `follow = false`.
- `dep` is removed.
- `runlocations` is emptied.
- `sendheaders` and `receiveheaders` lose each header removed from the map.
  NodePing merges these per header, so the provider names each removed one.
- `notifications`: an update always sends the whole list, which NodePing
  replaces, so removing the last block removes the last notification.
- `description`: NodePing ignores an empty description, so nothing clears
  one; it can only be overwritten. The provider overwrites it with a single
  space, which the resource, the data sources and an import all read as no
  description.
- `public` switches public reports off when set to `false` or left out. An
  update sends it as the string `"false"`: NodePing ignores the boolean.
- `fields`: a field, or a field's `min`, `max` or `match`, cannot be removed
  from an existing check. Removing one replaces the check; see
  [Parse Arguments](#parse-arguments).

Removing any other argument (`homeloc`, `port`, `username`, `secure`,
`verify`, the `dns*`, `snmp*` and database arguments, ...) has not been tried
against NodePing and is not cleared: NodePing keeps the value, and the apply
fails with "Provider produced inconsistent result after apply".

## Attribute Reference

- `id` - The unique identifier of the check.
- `customer_id` - The customer ID (account ID) that owns this check.
- `state` - Current state: `0` (failing) or `1` (passing).
- `created` - Creation timestamp (milliseconds).
- `modified` - Last modification timestamp (milliseconds).

## Supported Check Types

| Type | Description |
|------|-------------|
| `AGENT` | NodePing Agent check |
| `AUDIO` | Audio stream check |
| `CLUSTER` | Cluster of checks |
| `DNS` | DNS resolution check |
| `DOHDOT` | DNS over HTTPS/TLS |
| `FTP` | FTP server check |
| `HTTP` | HTTP/HTTPS check |
| `HTTPADV` | Advanced HTTP check |
| `HTTPCONTENT` | HTTP content match |
| `HTTPPARSE` | HTTP response parsing |
| `IMAP4` | IMAP mail server |
| `MONGODB` | MongoDB database |
| `MTR` | MTR traceroute |
| `MYSQL` | MySQL database |
| `NTP` | NTP time server |
| `PGSQL` | PostgreSQL database |
| `PING` | ICMP ping |
| `POP3` | POP3 mail server |
| `PORT` | TCP port check |
| `PUSH` | Push-based check |
| `RBL` | Real-time Blacklist |
| `RDAP` | RDAP domain lookup |
| `RDP` | Remote Desktop |
| `REDIS` | Redis database |
| `SIP` | SIP VoIP check |
| `SMTP` | SMTP mail server |
| `SNMP` | SNMP check |
| `SPEC10DNS` | SPEC10 DNS |
| `SPEC10RDDS` | SPEC10 RDDS |
| `SSH` | SSH server |
| `SSL` | SSL certificate |
| `WEBSOCKET` | WebSocket check |
| `WHOIS` | WHOIS domain lookup |

## Import

Checks can be imported using the check ID:

```shell
terraform import nodeping_check.example 201205050153W2Q4C-0J2HSIRF
```

For SubAccount checks, use the format `customer_id:check_id`:

```shell
terraform import nodeping_check.example 201205050153W2Q4C:201205050153W2Q4C-0J2HSIRF
```

## Notes

- Check IDs are generated by NodePing and cannot be set manually.
- Sub-minute intervals (0.25 and 0.5) may incur additional fees.
- The `dep` (dependency) feature prevents notifications when the dependent check is failing.
