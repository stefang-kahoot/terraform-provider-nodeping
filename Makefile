.PHONY: build test test-unit test-acceptance test-acceptance-live test-coverage docs lint clean docker-build docker-test docker-tidy

DOCKER_IMAGE := terraform-provider-nodeping
GO_VERSION := 1.27

build:
	docker run --rm -v $(PWD):/app -w /app golang:$(GO_VERSION)-alpine sh -c "go mod tidy && CGO_ENABLED=0 go build -o terraform-provider-nodeping ."

test:
	docker run --rm -v $(PWD):/app -w /app --env-file .env golang:$(GO_VERSION)-alpine sh -c "go mod tidy && go test -v ./..."

test-unit:
	docker run --rm -v $(PWD):/app -w /app golang:$(GO_VERSION)-alpine sh -c "go mod tidy && go test -v ./..."

# Acceptance tests drive a real terraform binary against an in-process mock of
# the NodePing API. The test image ships a pinned terraform, so no credentials
# and no calls to nodeping.com are involved.
test-acceptance:
	docker build --target test -t $(DOCKER_IMAGE):test .
	docker run --rm -e TF_ACC=1 $(DOCKER_IMAGE):test go test -v -timeout 20m -run "TestAcc" ./...

# The TestAccLive tests run against the real NodePing API and write to the
# account of NODEPING_API_TOKEN_ACCEPTANCE_TESTS: a dedicated test SubAccount,
# whose customer ID NODEPING_ACCEPTANCE_TESTS_CUSTOMER_ID gives. -e passes
# both from the environment, so neither shows on a command line.
test-acceptance-live:
	@test -n "$$NODEPING_API_TOKEN_ACCEPTANCE_TESTS" || { echo "set NODEPING_API_TOKEN_ACCEPTANCE_TESTS and NODEPING_ACCEPTANCE_TESTS_CUSTOMER_ID; see CONTRIBUTING.md" >&2; exit 1; }
	docker build --target test -t $(DOCKER_IMAGE):test .
	docker run --rm -e TF_ACC=1 -e NODEPING_API_TOKEN_ACCEPTANCE_TESTS -e NODEPING_ACCEPTANCE_TESTS_CUSTOMER_ID \
		$(DOCKER_IMAGE):test go test -v -count=1 -timeout 20m -run "TestAccLive" ./internal/provider/

# Regenerate the README version table from go.mod and the Dockerfile.
docs:
	./scripts/sync-readme-versions.sh

test-coverage:
	docker run --rm -v $(PWD):/app -w /app golang:$(GO_VERSION)-alpine sh -c "\
		go test -coverprofile=coverage.out -coverpkg=./... ./... && \
		go tool cover -func=coverage.out | tail -1"

tidy:
	docker run --rm -v $(PWD):/app -w /app golang:$(GO_VERSION)-alpine sh -c "go mod tidy"

# Pinned rather than :latest so a new linter release cannot fail a clean tree;
# scripts/check-pins.sh flags it when upstream moves on.
GOLANGCI_LINT_VERSION := v2.14.0

lint:
	docker run --rm -v $(PWD):/app -w /app golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint run

fmt:
	docker run --rm -v $(PWD):/app -w /app golang:$(GO_VERSION)-alpine sh -c "gofmt -w ."

docker-build:
	docker build --target builder -t $(DOCKER_IMAGE):builder .
	docker build --target runtime -t $(DOCKER_IMAGE):latest .

docker-test:
	docker build --target test -t $(DOCKER_IMAGE):test .
	docker run --rm --env-file .env $(DOCKER_IMAGE):test

clean:
	rm -f terraform-provider-nodeping
	docker rmi $(DOCKER_IMAGE):builder $(DOCKER_IMAGE):latest $(DOCKER_IMAGE):test 2>/dev/null || true

help:
	@echo "Available targets:"
	@echo "  build       - Build the provider binary using Docker"
	@echo "  test        - Run all tests using Docker"
	@echo "  test-unit   - Run unit tests only using Docker"
	@echo "  test-acceptance - Run terraform acceptance tests against the API mock"
	@echo "  test-acceptance-live - Run the acceptance tests that write to a real NodePing test account"
	@echo "  test-coverage   - Run tests and print total coverage"
	@echo "  docs        - Regenerate the README version table"
	@echo "  tidy        - Run go mod tidy using Docker"
	@echo "  lint        - Run golangci-lint using Docker"
	@echo "  fmt         - Format Go code using Docker"
	@echo "  docker-build - Build Docker images"
	@echo "  docker-test  - Run tests in Docker container"
	@echo "  clean       - Remove build artifacts and Docker images"
