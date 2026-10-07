package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

func (c *Client) ListContactGroups(ctx context.Context) (map[string]ContactGroup, error) {
	var result map[string]ContactGroup
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/contactgroups",
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to list contact groups: %w", err)
	}
	return result, nil
}

func (c *Client) GetContactGroup(ctx context.Context, id string) (*ContactGroup, error) {
	var result ContactGroup
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/contactgroups/" + url.PathEscape(id),
	}, &result)
	if err != nil {
		if apiErr, ok := errors.AsType[*APIError](err); ok && apiErr.IsNotFound() {
			return nil, &NotFoundError{ResourceType: "contact group", ResourceID: id}
		}
		return nil, fmt.Errorf("failed to get contact group: %w", err)
	}
	return &result, nil
}

func (c *Client) CreateContactGroup(ctx context.Context, req ContactGroupCreateRequest) (*ContactGroup, error) {
	var result ContactGroup
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPost,
		path:   "/contactgroups",
		body:   req,
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to create contact group: %w", err)
	}
	return &result, nil
}

// UpdateContactGroup sends the id both in the path and in the body. The API
// documents PUT against the bare /contactgroups endpoint with id as a
// parameter, while the other resources here address the object through the
// path; sending both satisfies either reading.
func (c *Client) UpdateContactGroup(ctx context.Context, id string, req ContactGroupUpdateRequest) (*ContactGroup, error) {
	req.ID = id

	var result ContactGroup
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPut,
		path:   "/contactgroups/" + url.PathEscape(id),
		body:   req,
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to update contact group: %w", err)
	}
	return &result, nil
}

func (c *Client) DeleteContactGroup(ctx context.Context, id string) error {
	var result DeleteResponse
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodDelete,
		path:   "/contactgroups/" + url.PathEscape(id),
	}, &result)
	if err != nil {
		if apiErr, ok := errors.AsType[*APIError](err); ok && apiErr.IsNotFound() {
			return &NotFoundError{ResourceType: "contact group", ResourceID: id}
		}
		return fmt.Errorf("failed to delete contact group: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("delete contact group returned ok=false")
	}
	return nil
}
