package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

const goneID = "201205050153W2Q4C-0J2HSIRF"

// goneResources holds NodePing's answers to a read and a delete of an ID it
// does not have, as the real API gave them on 2026-10-08.
var goneResources = []struct {
	name    string
	path    string
	getBody string
	delBody string
	get     func(*Client, context.Context, string) error
	del     func(*Client, context.Context, string) error
	create  func(*Client, context.Context) error
}{
	{
		name:    "check",
		path:    "/checks",
		getBody: `{"error":"Error fetching check."}`,
		delBody: `{"error":"Unable to find that check"}`,
		get: func(c *Client, ctx context.Context, id string) error {
			_, err := c.GetCheck(ctx, id)
			return err
		},
		del: (*Client).DeleteCheck,
		create: func(c *Client, ctx context.Context) error {
			_, err := c.CreateCheck(ctx, CheckCreateRequest{Type: "HTTP", Target: "https://example.com"})
			return err
		},
	},
	{
		name:    "contact",
		path:    "/contacts",
		getBody: `{}`,
		delBody: `{"error":"Unable to find that contact"}`,
		get: func(c *Client, ctx context.Context, id string) error {
			_, err := c.GetContact(ctx, id)
			return err
		},
		del: (*Client).DeleteContact,
		create: func(c *Client, ctx context.Context) error {
			_, err := c.CreateContact(ctx, ContactCreateRequest{Name: "new"})
			return err
		},
	},
	{
		name:    "contact group",
		path:    "/contactgroups",
		getBody: `{}`,
		delBody: `{"error":"Unable to find group"}`,
		get: func(c *Client, ctx context.Context, id string) error {
			_, err := c.GetContactGroup(ctx, id)
			return err
		},
		del: (*Client).DeleteContactGroup,
		create: func(c *Client, ctx context.Context) error {
			_, err := c.CreateContactGroup(ctx, ContactGroupCreateRequest{Name: "new"})
			return err
		},
	},
}

// goneServer answers any request for goneID with status and body, and the
// list with goneID in it or not; a listStatus other than 200 fails the list.
// It counts the list requests, atomically: the handler runs on the server's
// goroutine.
func goneServer(t *testing.T, path string, status int, body string, listed bool, listStatus int, lists *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case path:
			lists.Add(1)
			w.WriteHeader(listStatus)
			switch {
			case listStatus != http.StatusOK:
				_, _ = w.Write([]byte(`{"error":"bad request"}`))
			case listed:
				_, _ = w.Write([]byte(`{"` + goneID + `":{"_id":"` + goneID + `"}}`))
			default:
				_, _ = w.Write([]byte(`{}`))
			}
		case path + "/" + goneID:
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func goneClient(server *httptest.Server) *Client {
	return NewClient(ClientConfig{APIToken: "test-token", BaseURL: server.URL, RateLimit: 1000})
}

// A read or delete answered the way NodePing answers for an ID it does not
// have, of an ID the list does not have either, is not found.
func TestGoneIsNotFound(t *testing.T) {
	for _, res := range goneResources {
		for _, op := range []struct {
			name string
			body string
			call func(*Client, context.Context, string) error
		}{{"get", res.getBody, res.get}, {"delete", res.delBody, res.del}} {
			t.Run(res.name+" "+op.name, func(t *testing.T) {
				var lists atomic.Int32
				server := goneServer(t, res.path, http.StatusOK, op.body, false, http.StatusOK, &lists)

				err := op.call(goneClient(server), context.Background(), goneID)

				if _, ok := errors.AsType[*NotFoundError](err); !ok {
					t.Fatalf("expected *NotFoundError, got %T (%v)", err, err)
				}
				if got := lists.Load(); got != 1 {
					t.Errorf("expected 1 list request, got %d", got)
				}
			})
		}
	}
}

// The same answer for an ID that is still in the list is an error, not "gone":
// dropping the resource from state would plan a duplicate.
func TestGoneButListedIsAnError(t *testing.T) {
	for _, res := range goneResources {
		for _, op := range []struct {
			name string
			body string
			call func(*Client, context.Context, string) error
		}{{"get", res.getBody, res.get}, {"delete", res.delBody, res.del}} {
			t.Run(res.name+" "+op.name, func(t *testing.T) {
				var lists atomic.Int32
				server := goneServer(t, res.path, http.StatusOK, op.body, true, http.StatusOK, &lists)

				err := op.call(goneClient(server), context.Background(), goneID)

				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if _, ok := errors.AsType[*NotFoundError](err); ok {
					t.Fatalf("expected an error other than *NotFoundError, got %v", err)
				}
				if !mayBeGone(err) {
					t.Errorf("expected the error to keep NodePing's answer, got %v", err)
				}
			})
		}
	}
}

// When the list fails too, the answer stays an error and says both.
func TestGoneListFails(t *testing.T) {
	var lists atomic.Int32
	server := goneServer(t, "/checks", http.StatusOK, `{"error":"Error fetching check."}`, false, http.StatusBadRequest, &lists)

	_, err := goneClient(server).GetCheck(context.Background(), goneID)

	if _, ok := errors.AsType[*NotFoundError](err); ok || err == nil {
		t.Fatalf("expected an error other than *NotFoundError, got %v", err)
	}
	for _, want := range []string{"Error fetching check.", "listing", "bad request"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in %q", want, err.Error())
		}
	}
}

// A 404, should NodePing ever send one, is confirmed the same way.
func TestGone404IsNotFound(t *testing.T) {
	var lists atomic.Int32
	server := goneServer(t, "/checks", http.StatusNotFound, `{"error":"Check not found"}`, false, http.StatusOK, &lists)

	_, err := goneClient(server).GetCheck(context.Background(), goneID)

	if _, ok := errors.AsType[*NotFoundError](err); !ok {
		t.Fatalf("expected *NotFoundError, got %T (%v)", err, err)
	}
}

// Other errors are returned as they are, without listing anything.
func TestGoneOtherErrorsSkipTheList(t *testing.T) {
	var lists atomic.Int32
	server := goneServer(t, "/checks", http.StatusUnauthorized, `{"error":"Invalid token"}`, false, http.StatusOK, &lists)

	_, err := goneClient(server).GetCheck(context.Background(), goneID)

	if _, ok := errors.AsType[*NotFoundError](err); ok || err == nil {
		t.Fatalf("expected an error other than *NotFoundError, got %v", err)
	}
	if got := lists.Load(); got != 0 {
		t.Errorf("expected no list request, got %d", got)
	}
}

func expectNotFound(t *testing.T, err error) {
	t.Helper()
	if _, ok := errors.AsType[*NotFoundError](err); !ok {
		t.Errorf("expected *NotFoundError, got %T (%v)", err, err)
	}
}

// listingAPI answers like NodePing for the objects under one path: a read or
// delete of any ID with NodePing's answer for an ID it does not have, a
// create with a new object, and the list with the IDs in listed. It counts
// the lists, and fails the next failLists of them.
type listingAPI struct {
	t       *testing.T
	path    string
	getBody string
	delBody string

	mu           sync.Mutex
	listed       []string
	failLists    int
	createStatus int
	nLists       int
}

func newListingAPI(t *testing.T, path, getBody, delBody string) (*listingAPI, *Client) {
	t.Helper()
	api := &listingAPI{t: t, path: path, getBody: getBody, delBody: delBody, createStatus: http.StatusOK}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	return api, goneClient(server)
}

func (a *listingAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	item := strings.HasPrefix(r.URL.Path, a.path+"/")
	switch {
	case r.URL.Path == a.path && r.Method == http.MethodGet:
		a.nLists++
		if a.failLists > 0 {
			a.failLists--
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
			return
		}
		all := map[string]map[string]string{}
		for _, id := range a.listed {
			all[id] = map[string]string{"_id": id}
		}
		_ = json.NewEncoder(w).Encode(all)
	case r.URL.Path == a.path && r.Method == http.MethodPost:
		w.WriteHeader(a.createStatus)
		_, _ = w.Write([]byte(`{"_id":"201205050153W2Q4C-NEW"}`))
	case item && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(a.getBody))
	case item && r.Method == http.MethodDelete:
		_, _ = w.Write([]byte(a.delBody))
	default:
		a.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}
}

func (a *listingAPI) setListed(ids ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listed = ids
}

func (a *listingAPI) failNextLists(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failLists = n
}

func (a *listingAPI) setCreateStatus(status int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.createStatus = status
}

func (a *listingAPI) lists() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nLists
}

// One list answers every confirmation: after N objects were deleted in the
// web interface, a refresh lists once, not N times.
func TestGoneListsOnce(t *testing.T) {
	for _, res := range goneResources {
		t.Run(res.name, func(t *testing.T) {
			api, c := newListingAPI(t, res.path, res.getBody, res.delBody)
			ctx := context.Background()

			expectNotFound(t, res.get(c, ctx, "GONE-1"))
			expectNotFound(t, res.get(c, ctx, "GONE-2"))
			expectNotFound(t, res.get(c, ctx, "GONE-3"))
			expectNotFound(t, res.del(c, ctx, "GONE-4"))

			if got := api.lists(); got != 1 {
				t.Errorf("expected 1 list for 4 confirmations, got %d", got)
			}
		})
	}
}

// A create of the type drops its list: the new object is missing from a list
// taken before it, and a read of it that looks gone must not be taken for
// gone. A failed create drops it too, since NodePing may have carried it out.
func TestGoneListsAgainAfterACreate(t *testing.T) {
	for _, res := range goneResources {
		for _, tt := range []struct {
			name    string
			status  int
			wantErr bool
		}{
			{"created", http.StatusOK, false},
			{"create failed", http.StatusInternalServerError, true},
		} {
			t.Run(res.name+" "+tt.name, func(t *testing.T) {
				api, c := newListingAPI(t, res.path, res.getBody, res.delBody)
				api.setCreateStatus(tt.status)
				ctx := context.Background()

				expectNotFound(t, res.get(c, ctx, "GONE-1"))
				if err := res.create(c, ctx); (err != nil) != tt.wantErr {
					t.Fatalf("expected create error %v, got %v", tt.wantErr, err)
				}
				expectNotFound(t, res.get(c, ctx, "GONE-2"))
				expectNotFound(t, res.del(c, ctx, "GONE-3"))

				if got := api.lists(); got != 2 {
					t.Errorf("expected 1 list before the create and 1 after it, got %d", got)
				}
			})
		}
	}
}

// A failed list is not kept: the next confirmation lists again.
func TestGoneFailedListIsNotKept(t *testing.T) {
	api, c := newListingAPI(t, "/checks", `{"error":"Error fetching check."}`, `{"error":"Unable to find that check"}`)
	api.failNextLists(1)
	ctx := context.Background()

	_, err := c.GetCheck(ctx, "GONE-1")
	if _, ok := errors.AsType[*NotFoundError](err); ok || err == nil || !strings.Contains(err.Error(), "listing") {
		t.Fatalf("expected the failed list in the error, got %v", err)
	}
	_, err = c.GetCheck(ctx, "GONE-2")
	expectNotFound(t, err)

	if got := api.lists(); got != 2 {
		t.Errorf("expected the failed list and a second one, got %d", got)
	}
}

// An ID found in a list taken before its own read was answered may have been
// deleted since, so it is listed again before the read counts as an error.
func TestGoneListedEarlierIsListedAgain(t *testing.T) {
	for _, tt := range []struct {
		name         string
		deleted      bool
		wantNotFound bool
	}{
		{"still listed", false, false},
		{"deleted since", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api, c := newListingAPI(t, "/checks", `{"error":"Error fetching check."}`, `{"error":"Unable to find that check"}`)
			api.setListed("LISTED")
			ctx := context.Background()

			_, err := c.GetCheck(ctx, "GONE-1")
			expectNotFound(t, err)
			if tt.deleted {
				api.setListed()
			}
			_, err = c.GetCheck(ctx, "LISTED")

			if tt.wantNotFound {
				expectNotFound(t, err)
			} else {
				if _, ok := errors.AsType[*NotFoundError](err); ok || !mayBeGone(err) {
					t.Errorf("expected NodePing's answer as the error, got %v", err)
				}
			}
			if got := api.lists(); got != 2 {
				t.Errorf("expected 2 lists, got %d", got)
			}
		})
	}
}

// heldListAPI answers a read of any check the way NodePing answers for an ID
// it does not have, a create with a new check, and holds every list until
// release is closed or the request is canceled; the list is empty. It runs in
// memory, so that synctest can tell when every request is waiting.
type heldListAPI struct {
	release chan struct{}
	lists   atomic.Int32
}

func (h *heldListAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{"error":"Error fetching check."}`
	switch {
	case req.Method == http.MethodPost:
		body = `{"_id":"201205050153W2Q4C-NEW"}`
	case req.URL.Path == "/checks":
		h.lists.Add(1)
		select {
		case <-h.release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		body = `{}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func heldListClient() (*heldListAPI, *Client) {
	api := &heldListAPI{release: make(chan struct{})}
	c := NewClient(ClientConfig{APIToken: "test-token", BaseURL: "https://nodeping.test", RateLimit: math.Inf(1)})
	c.httpClient.Transport = api
	return api, c
}

// Confirmations at the same time wait for the one list in flight instead of
// each sending their own.
func TestGoneConfirmationsShareAListInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api, c := heldListClient()

		const n = 5
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				_, errs[i] = c.GetCheck(context.Background(), fmt.Sprintf("GONE-%d", i))
			})
		}
		synctest.Wait() // every confirmation waits: on the list, or for it
		if got := api.lists.Load(); got != 1 {
			t.Errorf("expected 1 list in flight for %d confirmations, got %d", n, got)
		}
		close(api.release)
		wg.Wait()

		for _, err := range errs {
			expectNotFound(t, err)
		}
		if got := api.lists.Load(); got != 1 {
			t.Errorf("expected 1 list in all, got %d", got)
		}
	})
}

// A list in flight when a create returns may lack the new object, so a
// confirmation that starts after the create sends a list of its own instead
// of waiting for that one.
func TestGoneCreateDropsAListInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api, c := heldListClient()

		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { _, errs[0] = c.GetCheck(context.Background(), "GONE-1") })
		synctest.Wait() // its list is in flight
		if _, err := c.CreateCheck(context.Background(), CheckCreateRequest{Type: "HTTP", Target: "https://example.com"}); err != nil {
			t.Errorf("create: %v", err)
		}
		wg.Go(func() { _, errs[1] = c.GetCheck(context.Background(), "GONE-2") })
		synctest.Wait()
		if got := api.lists.Load(); got != 2 {
			t.Errorf("expected a second list after the create, got %d lists", got)
		}
		close(api.release)
		wg.Wait()

		for _, err := range errs {
			expectNotFound(t, err)
		}
	})
}

// When the confirmation that sent the list gives up on it, one still waiting
// for it sends another instead of failing with it.
func TestGoneWaiterListsAgainWhenTheSenderGivesUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api, c := heldListClient()
		ctx, cancel := context.WithCancel(context.Background())

		var senderErr, waiterErr error
		var wg sync.WaitGroup
		wg.Go(func() { _, senderErr = c.GetCheck(ctx, "GONE-1") })
		synctest.Wait() // its list is in flight
		wg.Go(func() { _, waiterErr = c.GetCheck(context.Background(), "GONE-2") })
		synctest.Wait() // the second waits for it
		cancel()
		synctest.Wait()
		if got := api.lists.Load(); got != 2 {
			t.Errorf("expected the waiter to send a list of its own, got %d lists", got)
		}
		close(api.release)
		wg.Wait()

		if !errors.Is(senderErr, context.Canceled) {
			t.Errorf("expected the sender to fail with context.Canceled, got %v", senderErr)
		}
		expectNotFound(t, waiterErr)
	})
}

// A confirmation that gives up while waiting for a list returns at once, and
// the list goes on for the others.
func TestGoneWaiterGivesUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api, c := heldListClient()
		ctx, cancel := context.WithCancel(context.Background())

		var senderErr, waiterErr error
		var sender, waiter sync.WaitGroup
		sender.Go(func() { _, senderErr = c.GetCheck(context.Background(), "GONE-1") })
		synctest.Wait() // its list is in flight
		waiter.Go(func() { _, waiterErr = c.GetCheck(ctx, "GONE-2") })
		synctest.Wait() // the second waits for it
		cancel()
		waiter.Wait()

		if !errors.Is(waiterErr, context.Canceled) || !strings.Contains(waiterErr.Error(), "listing") {
			t.Errorf("expected the waiter to fail with context.Canceled while listing, got %v", waiterErr)
		}
		close(api.release)
		sender.Wait()
		expectNotFound(t, senderErr)
		if got := api.lists.Load(); got != 1 {
			t.Errorf("expected 1 list, got %d", got)
		}
	})
}
