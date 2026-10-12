package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (c *Client) ListContacts(ctx context.Context) (map[string]Contact, error) {
	var result map[string]Contact
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/contacts",
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to list contacts: %w", err)
	}
	for id, contact := range result {
		warnUnreadablePriorities(ctx, id, contact)
		warnRetypedFields(ctx, id, contact)
	}
	return result, nil
}

func (c *Client) GetContact(ctx context.Context, id string) (*Contact, error) {
	var result Contact
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/contacts/" + url.PathEscape(id),
	}, &result)
	if err == nil && result.ID == "" {
		err = errNoID
	}
	if err != nil {
		return nil, confirmGone(ctx, &c.contactList, c.ListContacts, "contact", id, fmt.Errorf("failed to get contact: %w", err))
	}
	warnUnreadablePriorities(ctx, id, result)
	warnRetypedFields(ctx, id, result)
	return &result, nil
}

func (c *Client) CreateContact(ctx context.Context, req ContactCreateRequest) (*Contact, error) {
	var result Contact
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPost,
		path:   "/contacts",
		body:   req,
	}, &result)
	// A list sent before now may lack the new contact, which even a failed
	// create may have added.
	c.contactList.created()
	if err != nil {
		return nil, createError("contact", req.Name, err)
	}
	warnUnreadablePriorities(ctx, result.ID, result)
	warnRetypedFields(ctx, result.ID, result)
	return &result, nil
}

func (c *Client) UpdateContact(ctx context.Context, id string, req ContactUpdateRequest) (*Contact, error) {
	var result Contact
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPut,
		path:   "/contacts/" + url.PathEscape(id),
		body:   req,
		// New addresses are added, so the update is not repeatable.
		addsSomething: len(req.NewAddresses) > 0,
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to update contact: %w", err)
	}
	warnUnreadablePriorities(ctx, id, result)
	warnRetypedFields(ctx, id, result)
	return &result, nil
}

func (c *Client) DeleteContact(ctx context.Context, id string) error {
	var result DeleteResponse
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodDelete,
		path:   "/contacts/" + url.PathEscape(id),
	}, &result)
	if err != nil {
		return confirmGone(ctx, &c.contactList, c.ListContacts, "contact", id, fmt.Errorf("failed to delete contact: %w", err))
	}
	if !result.OK {
		return fmt.Errorf("delete contact returned ok=false")
	}
	return nil
}

// warnUnreadablePriorities logs every address of the contact whose priority
// NodePing holds as something readPriority cannot read, and read as none. The
// log names the contact and the address, but leaves the value out.
func warnUnreadablePriorities(ctx context.Context, id string, contact Contact) {
	for addrID, addr := range contact.Addresses {
		if addr.unreadablePriority {
			tflog.Warn(ctx, "NodePing holds a contact address priority that is not a whole number; reading it as none", map[string]interface{}{
				"contact_id":   id,
				"contact_name": contact.Name,
				"address_id":   addrID,
			})
		}
	}
}
