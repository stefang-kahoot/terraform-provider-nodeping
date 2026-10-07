package client

import (
	"context"
	"encoding/json"
	"errors"
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

func TestGetContactNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "Contact not found"})
	}))
	defer server.Close()

	c := NewClient(ClientConfig{
		APIToken: "test-token",
		BaseURL:  server.URL,
	})

	_, err := c.GetContact(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	_, ok := errors.AsType[*NotFoundError](err)
	if !ok {
		t.Errorf("expected *NotFoundError, got %T", err)
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
