---
page_title: "nodeping_check Data Source - terraform-provider-nodeping"
subcategory: ""
description: |-
  Fetches a NodePing check by ID.
---

# nodeping_check (Data Source)

Fetches a NodePing check by ID, including the check-type specific parameters.

## Example Usage

```hcl
data "nodeping_check" "example" {
  id = "201205050153W2Q4C-0J2HSIRF"
}

output "check_target" {
  value = data.nodeping_check.example.target
}

output "expected_content" {
  value = data.nodeping_check.example.contentstring
}
```

## Argument Reference

- `id` - (Required) The unique identifier of the check.

## Attribute Reference

- `customer_id` - The customer ID (account ID) that owns this check.
- `type` - The check type, for example HTTP, PING or SSL.
- `target` - The target the check runs against.
- `label` - The display label of the check.
- `enabled` - Whether the check is enabled.
- `public` - Whether the check has a public reports page.
- `interval` - How often the check runs, in minutes.
- `threshold` - Timeout in seconds for the check.
- `sens` - Number of rechecks before the check is considered down.
- `mute` - Whether notifications for this check are muted.
- `autodiag` - Whether automatic diagnostics are enabled.
- `dep` - ID of the check this one depends on for notifications.
- `state` - Current state of the check (0 = failing, 1 = passing).
- `created` / `modified` - Timestamps in milliseconds.
- `description` - Free-form description of the check.
- `tags` - Tags assigned to the check.
- `runlocations` - Probe locations the check runs from.
- `homeloc` - Preferred probe location for the check.

### HTTP family

- `contentstring`, `regex`, `invert`, `follow`, `method`, `statuscode`,
  `sendheaders`, `receiveheaders`, `postdata`

### Parsed fields

- `fields` - Values parsed out of the response, keyed by NodePing's key for
  each field. Each entry contains `name`, `min`, `max` and `match`.

### Connection

- `port`, `username`, `secure`, `verify`, `ipv6`, `servername`, `transport`

### DNS

- `dnstype`, `dnstoresolve`, `dnssection`, `dnsrd`

### Certificates

- `warningdays`, `clientcert`

### Databases and services

- `email`, `database`, `query`, `namespace`, `sshkey`, `snmpv`

### Audio

- `verifyvolume`, `volumemin`

### Notifications

- `notifications` - Who is notified when the check changes state, in the order
  the API returns them. Each entry contains:
  - `contact_id` - ID of the notified contact or contact group.
  - `delay` - Minutes to wait before notifying.
  - `schedule` - Notification schedule the contact is notified on.

## Credentials

`password` and `snmpcom` are **not** exposed. A data source exists to be read,
and its values land in state and in plan output, so a stored secret has no
business there. `sshkey` and `clientcert` are exposed because the API returns
NodePing's *identifier* for a stored key, not the key material.
