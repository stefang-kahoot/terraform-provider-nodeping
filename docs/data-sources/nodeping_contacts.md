---
page_title: "nodeping_contacts Data Source - terraform-provider-nodeping"
subcategory: ""
description: |-
  Fetches all NodePing contacts.
---

# nodeping_contacts (Data Source)

Fetches all NodePing contacts.

## Example Usage

```hcl
data "nodeping_contacts" "all" {}

output "all_contact_ids" {
  value = [for c in data.nodeping_contacts.all.contacts : c.id]
}

output "all_contact_names" {
  value = [for c in data.nodeping_contacts.all.contacts : c.name]
}

# Find contacts by name
locals {
  ops_contacts = [
    for c in data.nodeping_contacts.all.contacts : c
    if can(regex("ops", lower(c.name)))
  ]
}
```

## Argument Reference

This data source has no required arguments.

## Attribute Reference

- `contacts` - All contacts, ordered by ID. Each contact contains:
  - `id` - The unique identifier of the contact.
  - `customer_id` - The customer ID (account ID) that owns this contact.
  - `name` - The name of the contact.
  - `custrole` - The permission role.
  - `addresses` - The contact's addresses, ordered by ID. Each address contains:
    - `id` - The unique identifier of the address.
    - `type` - The type of address.
    - `address` - The address value (sensitive).
