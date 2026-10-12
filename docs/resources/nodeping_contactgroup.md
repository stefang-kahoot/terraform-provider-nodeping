---
page_title: "nodeping_contactgroup Resource - terraform-provider-nodeping"
subcategory: ""
description: |-
  Manages a NodePing contact group.
---

# nodeping_contactgroup (Resource)

Manages a NodePing contact group.

A contact group bundles contact addresses so a check can notify all of them
through a single notification entry, instead of listing every address on every
check.

~> **Members are address IDs, not contact IDs.** A NodePing contact can hold
several addresses, and a group references those addresses individually. Use the
`id` of an `address` block, not the `id` of the `nodeping_contact` resource.

## Example Usage

```hcl
resource "nodeping_contact" "oncall" {
  name = "On-call"

  address {
    type    = "email"
    address = "oncall@example.com"
  }
}

resource "nodeping_contact" "backup" {
  name = "Backup"

  address {
    type    = "email"
    address = "backup@example.com"
  }
}

resource "nodeping_contactgroup" "escalation" {
  name = "Escalation"

  members = [
    nodeping_contact.oncall.address[0].id,
    nodeping_contact.backup.address[0].id,
  ]
}
```

### Notifying a group from a check

```hcl
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

### An empty group

`members` is optional; a group may be created empty and filled later.

```hcl
resource "nodeping_contactgroup" "placeholder" {
  name = "Placeholder"
}
```

## Argument Reference

- `name` - (Required) The display name of the contact group. It cannot be
  empty or only whitespace: NodePing can change a group's name, but not
  remove it.
- `members` - (Optional) List of contact **address** IDs belonging to this
  group. Setting this to `[]` removes every member.

## Attribute Reference

- `id` - The unique identifier of the contact group.
- `customer_id` - The customer ID (account ID) that owns this contact group.

## Import

Contact groups can be imported using the group ID:

```shell
terraform import nodeping_contactgroup.example 201205050153W2Q4C-G-1ZIYU
```

A group that NodePing holds without a name, created outside Terraform,
imports with an empty name. The plan after the import then updates it in
place to set the configuration's `name`.

To import a contact group from a SubAccount, give the resource a provider
configured with the SubAccount's `customer_id` and import it by its
plain ID, as above. That provider sends the customer ID with every request,
including the reads, updates and deletes after the import:

```hcl
provider "nodeping" {
  alias       = "subaccount"
  api_token   = var.nodeping_token
  customer_id = "SUBACCOUNT_ID"
}

resource "nodeping_contactgroup" "example" {
  provider = nodeping.subaccount
  # ...
}
```
