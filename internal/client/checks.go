package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

func (c *Client) ListChecks(ctx context.Context) (map[string]Check, error) {
	var result map[string]Check
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/checks",
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to list checks: %w", err)
	}
	return result, nil
}

func (c *Client) GetCheck(ctx context.Context, id string) (*Check, error) {
	var result Check
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/checks/" + url.PathEscape(id),
	}, &result)
	if err != nil {
		if apiErr, ok := errors.AsType[*APIError](err); ok && apiErr.IsNotFound() {
			return nil, &NotFoundError{ResourceType: "check", ResourceID: id}
		}
		return nil, fmt.Errorf("failed to get check: %w", err)
	}
	return &result, nil
}

func (c *Client) CreateCheck(ctx context.Context, req CheckCreateRequest) (*Check, error) {
	var result Check
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPost,
		path:   "/checks",
		body:   req,
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to create check: %w", err)
	}
	return &result, nil
}

func (c *Client) UpdateCheck(ctx context.Context, id string, req CheckUpdateRequest) (*Check, error) {
	var result Check
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPut,
		path:   "/checks/" + url.PathEscape(id),
		body:   req,
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to update check: %w", err)
	}
	return &result, nil
}

func (c *Client) DeleteCheck(ctx context.Context, id string) error {
	var result DeleteResponse
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodDelete,
		path:   "/checks/" + url.PathEscape(id),
	}, &result)
	if err != nil {
		return fmt.Errorf("failed to delete check: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("delete check returned ok=false")
	}
	return nil
}
