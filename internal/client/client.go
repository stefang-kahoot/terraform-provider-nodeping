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
	"net/http/httptrace"
	"net/url"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
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
	ignoreMute   bool

	// The lists that confirm a check, contact or contact group is gone,
	// shared for as long as the client lives. See listCache.
	checkList        listCache
	contactList      listCache
	contactGroupList listCache
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
	// IgnoreMute leaves the mute of a check whose configuration does not set
	// one to NodePing. See the provider's ignore_mute.
	IgnoreMute bool
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
		ignoreMute:   cfg.IgnoreMute,
	}
}

// WithCustomerID returns a client for the account customerID. It starts with
// lists of its own (see listCache), as those of c may be of another account.
// Creates through c do not drop them, which is safe while it only reads
// objects that existed before it, as ImportState does.
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
		ignoreMute:   c.ignoreMute,
	}
}

func (c *Client) GetDefaultTags() []string {
	return c.defaultTags
}

// IgnoreMute reports whether the provider's ignore_mute is set.
func (c *Client) IgnoreMute() bool {
	return c.ignoreMute
}

type requestOptions struct {
	method     string
	path       string
	query      url.Values
	body       interface{}
	customerID string
	// addsSomething marks a request other than a POST that adds something,
	// so that sending it twice could add it twice.
	addsSomething bool
}

// repeatable reports whether sending the request twice does no harm. A POST
// creates something, and so does any request marked addsSomething.
func (o requestOptions) repeatable() bool {
	return o.method != http.MethodPost && !o.addsSomething
}

// transportError is a request that got no answer from NodePing: the
// connection failed or dropped, or the response could not be read.
type transportError struct {
	err error
}

func (e *transportError) Error() string { return e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

// doRequest sends a request, retrying 429s, server errors and lost
// connections. A request that is not repeatable is retried only while it
// cannot have reached NodePing: on a 429, or when no connection was made. If
// it fails after it may have reached NodePing, the error is an
// UncertainWriteError, since NodePing may have carried it out.
func (c *Client) doRequest(ctx context.Context, opts requestOptions, result interface{}) error {
	var lastErr error
	sent := false
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			wait := c.retryWait(attempt, lastErr)
			tflog.Debug(ctx, "Retrying NodePing request", map[string]interface{}{
				"method":  opts.method,
				"path":    opts.path,
				"attempt": attempt,
				"wait":    wait.String(),
				"error":   lastErr.Error(),
			})
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		// Every attempt waits for the rate limiter, retries included.
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter: %w", err)
		}

		var err error
		sent, err = c.executeRequest(ctx, opts, result)
		if err == nil {
			return nil
		}
		lastErr = err

		if ctx.Err() != nil || !retryable(opts, sent, err) {
			break
		}
	}

	if !opts.repeatable() && sent && !answered(lastErr) {
		return &UncertainWriteError{Err: lastErr}
	}
	return lastErr
}

// retryable reports whether a failed attempt may be sent again.
func retryable(opts requestOptions, sent bool, err error) bool {
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		if apiErr.StatusCode == http.StatusTooManyRequests {
			return true
		}
		return apiErr.StatusCode >= 500 && opts.repeatable()
	}
	if _, ok := errors.AsType[*transportError](err); ok {
		return opts.repeatable() || !sent
	}
	return false
}

// answered reports whether err is NodePing's own answer that it did not carry
// out the request: an error status below 500 (429 included), or an error in
// a 200.
func answered(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.StatusCode < 500
}

// executeRequest sends one attempt. sent is false only if no connection to
// NodePing was made, so nothing can have reached it. Once a connection is
// made, Go's transport already resends a request it could not write at all
// to a reused connection; anything else may have arrived.
func (c *Client) executeRequest(ctx context.Context, opts requestOptions, result interface{}) (sent bool, err error) {
	reqURL, err := url.Parse(c.baseURL + opts.path)
	if err != nil {
		return false, fmt.Errorf("invalid URL: %w", err)
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
			return false, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	// Go calls GotConn on this goroutine, before writing anything, for
	// HTTP/1 and HTTP/2 alike.
	trace := &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { sent = true },
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), opts.method, reqURL.String(), bodyReader)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.apiToken, "")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if opts.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return sent, &transportError{fmt.Errorf("request failed: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return true, &transportError{fmt.Errorf("failed to read response body: %w", err)}
	}

	if resp.StatusCode >= 400 {
		return true, c.handleErrorResponse(resp, respBody)
	}

	// NodePing answers most failures with 200 and {"error": "..."}: a create
	// or update it rejects, and a read or delete of an ID it does not have.
	// Decoded as the result, that would pass for success.
	if msg := bodyError(respBody); msg != "" {
		return true, &APIError{
			StatusCode: resp.StatusCode,
			Message:    msg,
		}
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return true, fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return true, nil
}

func (c *Client) handleErrorResponse(resp *http.Response, body []byte) error {
	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Message:    string(body),
		RetryAfter: retryAfter(resp.Header),
	}
	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		apiErr.Message = errResp.Error
	}
	return apiErr
}

// retryAfter reads a Retry-After header, in seconds or as an HTTP date.
func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return max(time.Duration(secs)*time.Second, 0)
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return 0
}

// retryWait is how long to wait before the given attempt: the backoff, or
// what a 429 asked for if that is longer, but never more than retryMaxWait.
func (c *Client) retryWait(attempt int, lastErr error) time.Duration {
	wait := c.calculateBackoff(attempt)
	if apiErr, ok := errors.AsType[*APIError](lastErr); ok && apiErr.RetryAfter > wait {
		wait = min(apiErr.RetryAfter, c.retryMaxWait)
	}
	return wait
}

// bodyError returns the message of a top-level "error" in a JSON object, or
// "" if body has none.
func bodyError(body []byte) string {
	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return ""
	}
	return errResp.Error
}

func (c *Client) calculateBackoff(attempt int) time.Duration {
	backoff := float64(c.retryMinWait) * math.Pow(2, float64(attempt-1))
	if backoff > float64(c.retryMaxWait) {
		backoff = float64(c.retryMaxWait)
	}

	jitter := rand.Float64() * 0.3 * backoff
	return time.Duration(backoff + jitter)
}
