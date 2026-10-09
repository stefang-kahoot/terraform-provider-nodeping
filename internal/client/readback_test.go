package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// readBackServer answers every read of a check with the next modified of
// modifieds, the last one again once they run out, and counts the reads.
func readBackServer(t *testing.T, reads *atomic.Int32, modifieds ...int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(reads.Add(1))
		modified := modifieds[min(n, len(modifieds))-1]
		_, _ = fmt.Fprintf(w, `{"_id":"CHK","modified":%d}`, modified)
	}))
	t.Cleanup(server.Close)
	return server
}

func readBackClient(baseURL string, waits ...time.Duration) *Client {
	return NewClient(ClientConfig{
		APIToken:      "test-token",
		BaseURL:       baseURL,
		RateLimit:     1000,
		RetryMinWait:  time.Millisecond,
		RetryMaxWait:  2 * time.Millisecond,
		ReadBackWaits: waits,
	})
}

func newerThan(before int64) func(*Check) bool {
	return func(c *Check) bool { return c.Modified > before }
}

// Without read-back waits configured, they are 1, 2, 4 and 8 seconds: 15 s in
// all, longer than any stale answer seen so far took to clear.
func TestReadBackWaitsDefault(t *testing.T) {
	c := NewClient(ClientConfig{APIToken: "test-token"})
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if !slices.Equal(c.readBackWaits, want) {
		t.Errorf("readBackWaits = %v, want %v", c.readBackWaits, want)
	}
}

// The first read that shows the update ends the reading, after the waits
// before it.
func TestReadCheckBackStopsAtTheFirstAnswerShown(t *testing.T) {
	var reads atomic.Int32
	server := readBackServer(t, &reads, 100, 100, 200)
	c := readBackClient(server.URL, 20*time.Millisecond, 20*time.Millisecond, 20*time.Millisecond, 20*time.Millisecond)

	start := time.Now()
	check, err := c.ReadCheckBack(context.Background(), "CHK", newerThan(100))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check.Modified != 200 {
		t.Errorf("returned modified %d, want 200", check.Modified)
	}
	if got := reads.Load(); got != 3 {
		t.Errorf("read %d times, want 3", got)
	}
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Errorf("3 reads took %v; they did not wait 20ms each", elapsed)
	}
}

// When no read shows the update, the error says how often and how long it
// was read, after one read per wait.
func TestReadCheckBackGivesUp(t *testing.T) {
	var reads atomic.Int32
	server := readBackServer(t, &reads, 100)
	c := readBackClient(server.URL, time.Millisecond, 2*time.Millisecond, 3*time.Millisecond)

	check, err := c.ReadCheckBack(context.Background(), "CHK", newerThan(100))

	notShown, ok := errors.AsType[*UpdateNotShownError](err)
	if !ok {
		t.Fatalf("got %v, %v; want an UpdateNotShownError", check, err)
	}
	if notShown.ID != "CHK" || notShown.Reads != 3 || notShown.Waited != 6*time.Millisecond {
		t.Errorf("error = %+v, want ID CHK, 3 reads, 6ms", notShown)
	}
	if got := reads.Load(); got != 3 {
		t.Errorf("read %d times, want 3", got)
	}
}

// A context that ends during a wait ends the reading at once.
func TestReadCheckBackHonoursTheContext(t *testing.T) {
	var reads atomic.Int32
	server := readBackServer(t, &reads, 100)
	c := readBackClient(server.URL, time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.ReadCheckBack(ctx, "CHK", newerThan(100))

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want the context's error", err)
	}
	if got := reads.Load(); got != 0 {
		t.Errorf("read %d times, want 0", got)
	}
}

// A read that fails ends the reading with its error.
func TestReadCheckBackStopsAtAFailedRead(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"no access"}`))
	}))
	t.Cleanup(server.Close)
	c := readBackClient(server.URL, time.Millisecond, time.Millisecond)

	_, err := c.ReadCheckBack(context.Background(), "CHK", newerThan(100))

	if apiErr, ok := errors.AsType[*APIError](err); !ok || apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("got %v, want the read's 403", err)
	}
	if got := reads.Load(); got != 1 {
		t.Errorf("read %d times, want 1", got)
	}
}

// Every read waits for the rate limiter.
func TestReadCheckBackIsRateLimited(t *testing.T) {
	var reads atomic.Int32
	server := readBackServer(t, &reads, 100)
	c := readBackClient(server.URL, time.Millisecond, time.Millisecond, time.Millisecond)
	c.rateLimiter.SetLimit(4) // one request per 250ms

	start := time.Now()
	_, _ = c.ReadCheckBack(context.Background(), "CHK", newerThan(100))

	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Errorf("3 reads at 4 per second took %v; they skipped the rate limiter", elapsed)
	}
}
