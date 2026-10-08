package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	},
}

// goneServer answers any request for goneID with status and body, and the
// list with goneID in it or not; a listStatus other than 200 fails the list.
// It counts the list requests.
func goneServer(t *testing.T, path string, status int, body string, listed bool, listStatus int, lists *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case path:
			*lists++
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
				lists := 0
				server := goneServer(t, res.path, http.StatusOK, op.body, false, http.StatusOK, &lists)

				err := op.call(goneClient(server), context.Background(), goneID)

				if _, ok := errors.AsType[*NotFoundError](err); !ok {
					t.Fatalf("expected *NotFoundError, got %T (%v)", err, err)
				}
				if lists != 1 {
					t.Errorf("expected 1 list request, got %d", lists)
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
				lists := 0
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
	lists := 0
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
	lists := 0
	server := goneServer(t, "/checks", http.StatusNotFound, `{"error":"Check not found"}`, false, http.StatusOK, &lists)

	_, err := goneClient(server).GetCheck(context.Background(), goneID)

	if _, ok := errors.AsType[*NotFoundError](err); !ok {
		t.Fatalf("expected *NotFoundError, got %T (%v)", err, err)
	}
}

// Other errors are returned as they are, without listing anything.
func TestGoneOtherErrorsSkipTheList(t *testing.T) {
	lists := 0
	server := goneServer(t, "/checks", http.StatusUnauthorized, `{"error":"Invalid token"}`, false, http.StatusOK, &lists)

	_, err := goneClient(server).GetCheck(context.Background(), goneID)

	if _, ok := errors.AsType[*NotFoundError](err); ok || err == nil {
		t.Fatalf("expected an error other than *NotFoundError, got %v", err)
	}
	if lists != 0 {
		t.Errorf("expected no list request, got %d", lists)
	}
}
