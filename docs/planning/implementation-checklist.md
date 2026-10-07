# Implementation Checklist

**Timestamp**: 2026-01-27T08:40:00Z  
**Meta-prompt version**: v3.8

---

## Phase 4: Implementation

### 4.1 Project Setup
- [ ] Initialize Go module (`go mod init`)
- [ ] Create `go.mod` with pinned dependencies
- [ ] Create `.gitignore` (update existing)
- [ ] Create `.env.example`
- [ ] Create `main.go` entry point
- [ ] Run `go mod tidy`

### 4.2 HTTP Client (`internal/client/`)
- [ ] `models.go` - API data structures
  - [ ] Contact struct
  - [ ] ContactAddress struct
  - [ ] Check struct
  - [ ] APIError struct
- [ ] `errors.go` - Error types
  - [ ] APIError
  - [ ] NotFoundError
  - [ ] ValidationError
- [ ] `client.go` - HTTP client
  - [ ] Client struct with config
  - [ ] NewClient constructor
  - [ ] Rate limiter integration
  - [ ] Retry logic with exponential backoff
  - [ ] HTTP Basic Auth
  - [ ] Request/response logging (no secrets)
  - [ ] Error handling
- [ ] `contacts.go` - Contacts API
  - [ ] ListContacts
  - [ ] GetContact
  - [ ] CreateContact
  - [ ] UpdateContact
  - [ ] DeleteContact
- [ ] `checks.go` - Checks API
  - [ ] ListChecks
  - [ ] GetCheck
  - [ ] CreateCheck
  - [ ] UpdateCheck
  - [ ] DeleteCheck

### 4.3 Provider (`internal/provider/`)
- [ ] `provider.go` - Provider implementation
  - [ ] Provider struct
  - [ ] Schema (api_token, customer_id, api_url, rate_limit, retry config)
  - [ ] Configure method
  - [ ] Resources method
  - [ ] DataSources method
  - [ ] Metadata method

### 4.4 Contact Resource (`internal/resources/contact/`)
- [ ] `schema.go` - Schema definition
  - [ ] Contact attributes
  - [ ] Address block attributes
  - [ ] Sensitive field marking
- [ ] `resource.go` - Resource implementation
  - [ ] Create
  - [ ] Read
  - [ ] Update
  - [ ] Delete
  - [ ] ImportState
  - [ ] ValidateConfig

### 4.5 Check Resource (`internal/resources/check/`)
- [ ] `schema.go` - Schema definition
  - [ ] Common attributes
  - [ ] Type-specific attributes
  - [ ] Notifications block
  - [ ] Sensitive field marking
- [ ] `resource.go` - Resource implementation
  - [ ] Create
  - [ ] Read
  - [ ] Update
  - [ ] Delete
  - [ ] ImportState
  - [ ] ValidateConfig (type-aware validation)

### 4.6 Contact Data Sources (`internal/datasources/contact/`, `internal/datasources/contacts/`)
- [ ] `nodeping_contact` - Single contact
  - [ ] Schema
  - [ ] Read implementation
- [ ] `nodeping_contacts` - All contacts
  - [ ] Schema
  - [ ] Read implementation

### 4.7 Check Data Sources (`internal/datasources/check/`, `internal/datasources/checks/`)
- [ ] `nodeping_check` - Single check
  - [ ] Schema
  - [ ] Read implementation
- [ ] `nodeping_checks` - All checks
  - [ ] Schema with filters
  - [ ] Read implementation

---

## Phase 5: Tests

### 5.1 Unit Tests
- [ ] `client/client_test.go`
  - [ ] Auth header construction
  - [ ] Retry logic
  - [ ] Error handling
- [ ] `client/contacts_test.go`
  - [ ] CRUD operations with mock
- [ ] `client/checks_test.go`
  - [ ] CRUD operations with mock

### 5.2 Mock Server (`testutil/`)
- [ ] `mock_server.go`
  - [ ] Contacts endpoints
  - [ ] Checks endpoints
  - [ ] Error simulation
  - [ ] Rate limit simulation

### 5.3 Acceptance Tests
- [ ] `provider/provider_test.go`
  - [ ] Provider configuration
- [ ] `resources/contact/resource_test.go`
  - [ ] Create/Read/Update/Delete
  - [ ] Import
  - [ ] Address management
- [ ] `resources/check/resource_test.go`
  - [ ] Create/Read/Update/Delete per type
  - [ ] Import
  - [ ] Notifications

### 5.4 Test Fixtures (`testutil/fixtures/`)
- [ ] Sample contact JSON
- [ ] Sample check JSON (various types)

---

## Phase 6: Documentation

### 6.1 README.md
- [ ] Project overview
- [ ] Installation instructions
- [ ] Provider configuration
- [ ] Authentication (config + env vars)
- [ ] Multi-account usage (aliases)
- [ ] Quick start examples
- [ ] Security considerations
- [ ] Contributing guidelines

### 6.2 Resource Documentation (`docs/resources/`)
- [ ] `nodeping_contact.md`
- [ ] `nodeping_check.md`

### 6.3 Data Source Documentation (`docs/data-sources/`)
- [ ] `nodeping_contact.md`
- [ ] `nodeping_contacts.md`
- [ ] `nodeping_check.md`
- [ ] `nodeping_checks.md`

### 6.4 Examples (`examples/`)
- [ ] Provider configuration
- [ ] Contact resource examples
- [ ] Check resource examples (multiple types)
- [ ] Data source examples
- [ ] Multi-account example

---

## CI/CD Setup

### GitHub Actions (`.github/workflows/`)
- [x] `test.yml` - build/vet/gofmt, unit tests with race + coverage, acceptance tests
- [x] `security.yml` - gitleaks secret detection, Trivy vuln/misconfig/secret scanning
- [x] `release.yml` - goreleaser on tag push

### Repository Setup
- [ ] CODEOWNERS
- [ ] Protected branches (main)
- [ ] Protected tags (v*)
- [ ] Merge request templates

---

## Verification Gates

Before marking complete:
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] `golangci-lint run` passes
- [ ] `gosec ./...` passes (or documented exceptions)
- [ ] `go mod tidy` produces no changes
- [ ] All examples validate with `terraform validate`
