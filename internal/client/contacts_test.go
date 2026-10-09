package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListContacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/contacts" {
			t.Errorf("expected path /contacts, got %s", r.URL.Path)
		}

		contacts := map[string]Contact{
			"201205050153W2Q4C-BKPGH": {
				ID:         "201205050153W2Q4C-BKPGH",
				CustomerID: "201205050153W2Q4C",
				Name:       "Test Contact",
				CustRole:   "notify",
				Addresses: map[string]ContactAddress{
					"K5SP9CQP": {
						Address: "test@example.com",
						Type:    "email",
					},
				},
			},
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(contacts)
	}))
	defer server.Close()

	c := NewClient(ClientConfig{
		APIToken: "test-token",
		BaseURL:  server.URL,
	})

	contacts, err := c.ListContacts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(contacts) != 1 {
		t.Errorf("expected 1 contact, got %d", len(contacts))
	}

	contact, ok := contacts["201205050153W2Q4C-BKPGH"]
	if !ok {
		t.Fatal("expected contact not found")
	}

	if contact.Name != "Test Contact" {
		t.Errorf("expected name 'Test Contact', got %q", contact.Name)
	}
}

func TestGetContact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		contact := Contact{
			ID:         "201205050153W2Q4C-BKPGH",
			CustomerID: "201205050153W2Q4C",
			Name:       "Test Contact",
			CustRole:   "notify",
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(contact)
	}))
	defer server.Close()

	c := NewClient(ClientConfig{
		APIToken: "test-token",
		BaseURL:  server.URL,
	})

	contact, err := c.GetContact(context.Background(), "201205050153W2Q4C-BKPGH")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if contact.ID != "201205050153W2Q4C-BKPGH" {
		t.Errorf("expected ID '201205050153W2Q4C-BKPGH', got %q", contact.ID)
	}
}

func TestCreateContact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req ContactCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Name != "New Contact" {
			t.Errorf("expected name 'New Contact', got %q", req.Name)
		}

		contact := Contact{
			ID:         "201205050153W2Q4C-NEWID",
			CustomerID: "201205050153W2Q4C",
			Name:       req.Name,
			CustRole:   req.CustRole,
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(contact)
	}))
	defer server.Close()

	c := NewClient(ClientConfig{
		APIToken: "test-token",
		BaseURL:  server.URL,
	})

	contact, err := c.CreateContact(context.Background(), ContactCreateRequest{
		Name:     "New Contact",
		CustRole: "notify",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if contact.ID != "201205050153W2Q4C-NEWID" {
		t.Errorf("expected ID '201205050153W2Q4C-NEWID', got %q", contact.ID)
	}
}

func TestUpdateContact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}

		contact := Contact{
			ID:         "201205050153W2Q4C-BKPGH",
			CustomerID: "201205050153W2Q4C",
			Name:       "Updated Contact",
			CustRole:   "edit",
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(contact)
	}))
	defer server.Close()

	c := NewClient(ClientConfig{
		APIToken: "test-token",
		BaseURL:  server.URL,
	})

	contact, err := c.UpdateContact(context.Background(), "201205050153W2Q4C-BKPGH", ContactUpdateRequest{
		Name:     "Updated Contact",
		CustRole: "edit",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if contact.Name != "Updated Contact" {
		t.Errorf("expected name 'Updated Contact', got %q", contact.Name)
	}
}

func TestDeleteContact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(DeleteResponse{OK: true, ID: "201205050153W2Q4C-BKPGH"})
	}))
	defer server.Close()

	c := NewClient(ClientConfig{
		APIToken: "test-token",
		BaseURL:  server.URL,
	})

	err := c.DeleteContact(context.Background(), "201205050153W2Q4C-BKPGH")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// sentFields marshals a request and returns its top-level fields as sent.
func sentFields(t *testing.T, req any) map[string]json.RawMessage {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return fields
}

// An update that sends `addresses` replaces the contact's addresses with
// them, so a replacement of every address has to send it even when it keeps
// none: {} with the new ones under `newaddresses`. An update with no address
// to send leaves both keys out; NodePing refuses an empty collection that
// would leave the contact with no address. null would happen to be ignored,
// but leaving the key out says so.
func TestContactUpdateRequestAddresses(t *testing.T) {
	t.Parallel()

	email := AddressRequest{Type: "email", Address: "x@example.com"}
	tests := []struct {
		name          string
		req           ContactUpdateRequest
		wantAddresses string
		wantNew       bool
	}{
		{"nil leaves addresses out", ContactUpdateRequest{Name: "n"}, "", false},
		{"an empty map replaces every address", ContactUpdateRequest{Addresses: map[string]AddressRequest{}, NewAddresses: []AddressRequest{email}}, `{}`, true},
		{"kept addresses go by ID", ContactUpdateRequest{Addresses: map[string]AddressRequest{"A1": {Type: "email", Address: "a@example.com"}}}, `{"A1":{"address":"a@example.com","type":"email"}}`, false},
		{"no new addresses leaves newaddresses out", ContactUpdateRequest{Addresses: map[string]AddressRequest{}, NewAddresses: []AddressRequest{}}, `{}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fields := sentFields(t, tt.req)
			if got := string(fields["addresses"]); got != tt.wantAddresses {
				t.Errorf("addresses = %q, want %q", got, tt.wantAddresses)
			}
			if _, sent := fields["newaddresses"]; sent != tt.wantNew {
				t.Errorf("newaddresses sent: %v, want %v", sent, tt.wantNew)
			}
		})
	}
}

// NodePing refuses a create with `newaddresses: []`, but creates a contact
// with no address when the key is left out.
func TestContactCreateRequestLeavesOutNoNewAddresses(t *testing.T) {
	t.Parallel()

	for _, req := range []ContactCreateRequest{
		{Name: "n"},
		{Name: "n", NewAddresses: []AddressRequest{}},
	} {
		if got, sent := sentFields(t, req)["newaddresses"]; sent {
			t.Errorf("a create with no addresses sent newaddresses %s", got)
		}
	}
}
