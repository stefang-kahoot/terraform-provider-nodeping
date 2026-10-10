---
page_title: "nodeping_contact Data Source - terraform-provider-nodeping"
subcategory: ""
description: |-
  Fetches a NodePing contact by ID.
---

# nodeping_contact (Data Source)

Fetches a NodePing contact by ID.

## Example Usage

```hcl
data "nodeping_contact" "example" {
  id = "201205050153W2Q4C-BKPGH"
}

output "contact_name" {
  value = data.nodeping_contact.example.name
}

output "contact_role" {
  value = data.nodeping_contact.example.custrole
}
```

## Argument Reference

- `id` - (Required) The unique identifier of the contact.

## Attribute Reference

- `customer_id` - The customer ID (account ID) that owns this contact.
- `name` - The name of the contact.
- `custrole` - The permission role: `edit`, `view`, or `notify`.
- `addresses` - The contact's addresses, ordered by ID. Each address contains:
  - `id` - The unique identifier of the address.
  - `type` - The type of address.
  - `address` - The address value (sensitive).
  - `suppress_up` - Whether "up" notifications are suppressed.
  - `suppress_down` - Whether "down" notifications are suppressed.
  - `suppress_first` - Whether "first result" notifications are suppressed.
  - `suppress_diag` - Whether diagnostic notifications are suppressed.
  - `suppress_all` - Whether all notifications are suppressed.
