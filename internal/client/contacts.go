package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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
		return nil, confirmGone(ctx, c.ListContacts, "contact", id, fmt.Errorf("failed to get contact: %w", err))
	}
	return &result, nil
}

func (c *Client) CreateContact(ctx context.Context, req ContactCreateRequest) (*Contact, error) {
	var result Contact
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPost,
		path:   "/contacts",
		body:   req,
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to create contact: %w", err)
	}
	return &result, nil
}

func (c *Client) UpdateContact(ctx context.Context, id string, req ContactUpdateRequest) (*Contact, error) {
	var result Contact
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPut,
		path:   "/contacts/" + url.PathEscape(id),
		body:   req,
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to update contact: %w", err)
	}
	return &result, nil
}

func (c *Client) DeleteContact(ctx context.Context, id string) error {
	var result DeleteResponse
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodDelete,
		path:   "/contacts/" + url.PathEscape(id),
	}, &result)
	if err != nil {
		return confirmGone(ctx, c.ListContacts, "contact", id, fmt.Errorf("failed to delete contact: %w", err))
	}
	if !result.OK {
		return fmt.Errorf("delete contact returned ok=false")
	}
	return nil
}
