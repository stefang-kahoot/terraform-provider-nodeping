package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flakyTransport fails the first failures attempts with a transport error,
// after making a connection or not, and then answers 200 with body.
type flakyTransport struct {
	failures int
	connect  bool
	body     string
	attempts int
}

func (f *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.attempts++
	if f.attempts <= f.failures {
		if trace := httptrace.ContextClientTrace(req.Context()); f.connect && trace != nil && trace.GotConn != nil {
			trace.GotConn(httptrace.GotConnInfo{})
		}
		return nil, io.EOF
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Request:    req,
	}, nil
}

func retryClient(baseURL string, maxRetries int) *Client {
	return NewClient(ClientConfig{
		APIToken:     "test-token",
		BaseURL:      baseURL,
		RateLimit:    1000,
		MaxRetries:   maxRetries,
		RetryMinWait: time.Millisecond,
		RetryMaxWait: 2 * time.Millisecond,
	})
}

// A transport error is retried for a repeatable request, and for one that
// adds something only while no connection was made: once there was one, the
// request may have reached NodePing, and sending it again could add the same
// thing twice.
func TestDoRequestRetriesTransportErrors(t *testing.T) {
	tests := []struct {
		name          string
		opts          requestOptions
		failures      int
		connect       bool
		wantAttempts  int
		wantErr       bool
		wantUncertain bool
	}{
		{"GET after a dropped connection", requestOptions{method: http.MethodGet}, 1, true, 2, false, false},
		{"PUT after a dropped connection", requestOptions{method: http.MethodPut}, 1, true, 2, false, false},
		{"DELETE after a dropped connection", requestOptions{method: http.MethodDelete}, 1, true, 2, false, false},
		{"POST that never connected", requestOptions{method: http.MethodPost}, 1, false, 2, false, false},
		{"POST after a dropped connection", requestOptions{method: http.MethodPost}, 1, true, 1, true, true},
		{"PUT adding addresses after a dropped connection", requestOptions{method: http.MethodPut, addsSomething: true}, 1, true, 1, true, true},
		{"PUT adding addresses that never connected", requestOptions{method: http.MethodPut, addsSomething: true}, 1, false, 2, false, false},
		{"GET failing every attempt", requestOptions{method: http.MethodGet}, 99, true, 4, true, false},
		{"POST never connecting", requestOptions{method: http.MethodPost}, 99, false, 4, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &flakyTransport{failures: tt.failures, connect: tt.connect, body: `{"_id":"X"}`}
			c := retryClient("https://api.example.test/api/1", 3)
			c.httpClient.Transport = transport
			tt.opts.path = "/checks"

			var result Check
			err := c.doRequest(context.Background(), tt.opts, &result)

			if transport.attempts != tt.wantAttempts {
				t.Errorf("expected %d attempts, got %d", tt.wantAttempts, transport.attempts)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if _, ok := errors.AsType[*UncertainWriteError](err); ok != tt.wantUncertain {
				t.Errorf("expected UncertainWriteError %v, got %T (%v)", tt.wantUncertain, err, err)
			}
		})
	}
}

// Error statuses: 429 means NodePing did nothing, so anything is retried; a
// server error is retried only for a repeatable request, and for one that
// adds something it leaves the outcome unknown.
func TestDoRequestRetriesErrorStatuses(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		status        int
		body          string
		wantAttempts  int
		wantErr       bool
		wantUncertain bool
	}{
		{"POST answered 429", http.MethodPost, http.StatusTooManyRequests, `{"error":"slow down"}`, 2, false, false},
		{"POST answered 500", http.MethodPost, http.StatusInternalServerError, `{"error":"bug"}`, 1, true, true},
		{"PUT answered 500", http.MethodPut, http.StatusInternalServerError, `{"error":"bug"}`, 2, false, false},
		{"POST answered 400", http.MethodPost, http.StatusBadRequest, `{"error":"bad"}`, 1, true, false},
		{"POST rejected with 200", http.MethodPost, http.StatusOK, `{"error":"target: Invalid URL"}`, 1, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if attempts.Add(1) == 1 {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
					return
				}
				_, _ = w.Write([]byte(`{"_id":"X"}`))
			}))
			t.Cleanup(server.Close)

			var result Check
			err := retryClient(server.URL, 3).doRequest(context.Background(), requestOptions{method: tt.method, path: "/checks"}, &result)

			if got := int(attempts.Load()); got != tt.wantAttempts {
				t.Errorf("expected %d attempts, got %d", tt.wantAttempts, got)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if _, ok := errors.AsType[*UncertainWriteError](err); ok != tt.wantUncertain {
				t.Errorf("expected UncertainWriteError %v, got %T (%v)", tt.wantUncertain, err, err)
			}
		})
	}
}

// With no retries, a request that fails is sent once. Less than 0 retries
// still sends it, rather than not at all.
func TestDoRequestWithoutRetries(t *testing.T) {
	for _, maxRetries := range []int{0, -1} {
		t.Run(strconv.Itoa(maxRetries), func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"bug"}`))
			}))
			t.Cleanup(server.Close)

			_, err := retryClient(server.URL, maxRetries).GetCheck(context.Background(), "X")

			if got := attempts.Load(); got != 1 {
				t.Errorf("expected 1 attempt, got %d", got)
			}
			if apiErr, ok := errors.AsType[*APIError](err); !ok || apiErr.StatusCode != http.StatusInternalServerError {
				t.Errorf("expected the 500, got %T (%v)", err, err)
			}
		})
	}
}

// dropFirst closes the connection without answering the first request, after
// reading it, the way NodePing's end dropped some in October 2026. The
// handler runs on the server's goroutine, so it counts atomically.
func dropFirst(t *testing.T, attempts *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			_, _ = io.ReadAll(r.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(`{"_id":"X","label":"acc"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDoRequestRetriesARealDroppedConnection(t *testing.T) {
	var attempts atomic.Int32
	server := dropFirst(t, &attempts)

	_, err := retryClient(server.URL, 3).GetCheck(context.Background(), "X")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

// A create whose connection drops is not sent again, and the error names the
// check and says it may exist.
func TestCreateCheckAfterADroppedConnection(t *testing.T) {
	var attempts atomic.Int32
	server := dropFirst(t, &attempts)

	_, err := retryClient(server.URL, 3).CreateCheck(context.Background(), CheckCreateRequest{Type: "HTTP", Label: "acc-dropped"})

	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 attempt, got %d", got)
	}
	if _, ok := errors.AsType[*UncertainWriteError](err); !ok {
		t.Fatalf("expected UncertainWriteError, got %T (%v)", err, err)
	}
	for _, want := range []string{`"acc-dropped"`, "may have created it", "import"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in %q", want, err.Error())
		}
	}
}

// Every attempt waits for the rate limiter, retries included.
func TestDoRequestRateLimitsRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	c := retryClient(server.URL, 3)
	c.rateLimiter.SetLimit(4) // one request per 250ms

	start := time.Now()
	var result map[string]interface{}
	err := c.doRequest(context.Background(), requestOptions{method: http.MethodGet, path: "/checks"}, &result)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Errorf("3 attempts at 4 per second took %v; the retries skipped the rate limiter", elapsed)
	}
}

// A 429's Retry-After is waited for, up to retryMaxWait.
func TestDoRequestHonoursRetryAfter(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter string
		maxWait    time.Duration
		atLeast    time.Duration
		atMost     time.Duration
	}{
		{"seconds", "1", 5 * time.Second, 900 * time.Millisecond, 3 * time.Second},
		{"capped by retry_wait_max", "120", 50 * time.Millisecond, 0, time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if attempts.Add(1) == 1 {
					w.Header().Set("Retry-After", tt.retryAfter)
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(server.Close)

			c := retryClient(server.URL, 3)
			c.retryMaxWait = tt.maxWait

			start := time.Now()
			var result map[string]interface{}
			err := c.doRequest(context.Background(), requestOptions{method: http.MethodGet, path: "/checks"}, &result)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if elapsed < tt.atLeast || elapsed > tt.atMost {
				t.Errorf("took %v, want between %v and %v", elapsed, tt.atLeast, tt.atMost)
			}
		})
	}
}

func TestRetryAfter(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		atLeast time.Duration
		atMost  time.Duration
	}{
		{"absent", "", 0, 0},
		{"seconds", "7", 7 * time.Second, 7 * time.Second},
		{"negative", "-3", 0, 0},
		{"HTTP date", time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat), 8 * time.Second, 10 * time.Second},
		{"date in the past", time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat), 0, 0},
		{"garbage", "soon", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.value != "" {
				h.Set("Retry-After", tt.value)
			}
			if got := retryAfter(h); got < tt.atLeast || got > tt.atMost {
				t.Errorf("retryAfter(%q) = %v, want between %v and %v", tt.value, got, tt.atLeast, tt.atMost)
			}
		})
	}
}
