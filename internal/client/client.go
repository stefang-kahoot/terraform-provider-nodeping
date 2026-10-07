package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/time/rate"
)

const (
	DefaultBaseURL      = "https://api.nodeping.com/api/1"
	DefaultRateLimit    = 10
	DefaultMaxRetries   = 3
	DefaultRetryMinWait = 1 * time.Second
	DefaultRetryMaxWait = 30 * time.Second
	DefaultTimeout      = 30 * time.Second
)

type Client struct {
	httpClient   *http.Client
	baseURL      string
	apiToken     string
	customerID   string
	rateLimiter  *rate.Limiter
	maxRetries   int
	retryMinWait time.Duration
	retryMaxWait time.Duration
	userAgent    string
	defaultTags  []string
}

type ClientConfig struct {
	APIToken     string
	CustomerID   string
	BaseURL      string
	RateLimit    float64
	MaxRetries   int
	RetryMinWait time.Duration
	RetryMaxWait time.Duration
	Timeout      time.Duration
	UserAgent    string
	DefaultTags  []string
}

func NewClient(cfg ClientConfig) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = DefaultRateLimit
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.RetryMinWait <= 0 {
		cfg.RetryMinWait = DefaultRetryMinWait
	}
	if cfg.RetryMaxWait <= 0 {
		cfg.RetryMaxWait = DefaultRetryMaxWait
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "terraform-provider-nodeping"
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		baseURL:      cfg.BaseURL,
		apiToken:     cfg.APIToken,
		customerID:   cfg.CustomerID,
		rateLimiter:  rate.NewLimiter(rate.Limit(cfg.RateLimit), 1),
		maxRetries:   cfg.MaxRetries,
		retryMinWait: cfg.RetryMinWait,
		retryMaxWait: cfg.RetryMaxWait,
		userAgent:    cfg.UserAgent,
		defaultTags:  cfg.DefaultTags,
	}
}

func (c *Client) WithCustomerID(customerID string) *Client {
	return &Client{
		httpClient:   c.httpClient,
		baseURL:      c.baseURL,
		apiToken:     c.apiToken,
		customerID:   customerID,
		rateLimiter:  c.rateLimiter,
		maxRetries:   c.maxRetries,
		retryMinWait: c.retryMinWait,
		retryMaxWait: c.retryMaxWait,
		userAgent:    c.userAgent,
		defaultTags:  c.defaultTags,
	}
}

func (c *Client) GetDefaultTags() []string {
	return c.defaultTags
}

type requestOptions struct {
	method     string
	path       string
	query      url.Values
	body       interface{}
	customerID string
}

func (c *Client) doRequest(ctx context.Context, opts requestOptions, result interface{}) error {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			waitTime := c.calculateBackoff(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitTime):
			}
		}

		err := c.executeRequest(ctx, opts, result)
		if err == nil {
			return nil
		}

		lastErr = err

		if apiErr, ok := errors.AsType[*APIError](err); ok {
			if !apiErr.IsRetryable() {
				return err
			}
		} else {
			return err
		}
	}

	return lastErr
}

func (c *Client) executeRequest(ctx context.Context, opts requestOptions, result interface{}) error {
	reqURL, err := url.Parse(c.baseURL + opts.path)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if opts.query == nil {
		opts.query = url.Values{}
	}

	customerID := opts.customerID
	if customerID == "" {
		customerID = c.customerID
	}
	if customerID != "" {
		opts.query.Set("customerid", customerID)
	}

	reqURL.RawQuery = opts.query.Encode()

	var bodyReader io.Reader
	if opts.body != nil {
		jsonData, err := json.Marshal(opts.body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, opts.method, reqURL.String(), bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.apiToken, "")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if opts.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return c.handleErrorResponse(resp.StatusCode, respBody)
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

func (c *Client) handleErrorResponse(statusCode int, body []byte) error {
	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return &APIError{
			StatusCode: statusCode,
			Message:    errResp.Error,
		}
	}

	return &APIError{
		StatusCode: statusCode,
		Message:    string(body),
	}
}

func (c *Client) calculateBackoff(attempt int) time.Duration {
	backoff := float64(c.retryMinWait) * math.Pow(2, float64(attempt-1))
	if backoff > float64(c.retryMaxWait) {
		backoff = float64(c.retryMaxWait)
	}

	jitter := rand.Float64() * 0.3 * backoff
	return time.Duration(backoff + jitter)
}
