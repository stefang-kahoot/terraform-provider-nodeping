# Contributing

This provider is maintained by one person in their spare time. Issues and pull
requests may get a slow response or none, and there is no roadmap for outside
contributions. This file describes what CI checks, so a pull request that does
arrive can be judged on a green run.

Report security issues privately as described in [SECURITY.md](SECURITY.md),
not in a public issue.

## Local checks

All of these run in CI.

```bash
go build ./...
go test ./...                              # unit tests
TF_ACC=1 go test -run TestAcc ./...        # acceptance tests (needs terraform on PATH)
make lint                                  # golangci-lint at the pinned version, via Docker
./scripts/sync-readme-versions.sh --check  # README version table is current
go mod tidy && git diff --exit-code -- go.mod go.sum
```

The acceptance tests drive a real `terraform` binary against an in-process mock
of the NodePing API (`testutil/mock_server.go`). They need no NodePing account
or API token and make no calls to nodeping.com. `make test-acceptance` runs them
in Docker with the Terraform version pinned in the `Dockerfile`.

`make lint` uses `GOLANGCI_LINT_VERSION` from the `Makefile`, which CI asserts
matches the version in `.github/workflows/ci.yml`. The linter set is in
`.golangci.yml`: the standard set plus `errorlint`, `gosec` and the `gofmt`
formatter.

If a dependency bump changes a version in the README's **Pinned Versions**
table, regenerate it with `make docs` and commit the result.

If you change a workflow, CI also runs `actionlint` and `zizmor --offline` over
`.github/`. Third-party actions must be pinned to a full commit SHA with a
trailing version comment (`.github/zizmor.yml`).

## Tests against the NodePing API

The `TestAccLive*` tests check the provider against the real NodePing API
rather than the mock. For each resource they create one object, update it in
place, import it and destroy it. They write to the account, so they are opt-in
and CI does not run them. Without these variables they are skipped:

- `NODEPING_API_TOKEN_ACCEPTANCE_TESTS`: the API token of a dedicated test
  SubAccount. Never use the token of an account that monitors anything.
- `NODEPING_ACCEPTANCE_TESTS_CUSTOMER_ID`: that SubAccount's customer ID. If
  the token is set and this is not, the tests fail.

Before writing anything, each test lists the account's checks, contacts and
contact groups, and stops unless every ID starts with that customer ID. Each
object a test creates is named `tf-acc-live-<random>` and destroyed at the
end, but a failed run can leave some behind to delete by hand. The provider
sees only the acceptance token: your own `NODEPING_API_TOKEN`,
`NODEPING_API_KEY`, `NODEPING_CUSTOMER_ID` and `NODEPING_API_URL` are ignored
for the run.

Set the variables for the one command, so that a later
`TF_ACC=1 go test ./...` does not write to the account as well. `-count=1`
stops `go test` from reporting a cached result without calling NodePing:

```bash
TF_ACC=1 \
NODEPING_API_TOKEN_ACCEPTANCE_TESTS="$(cat path/to/test-subaccount-token)" \
NODEPING_ACCEPTANCE_TESTS_CUSTOMER_ID='your-customer-id-here' \
go test -v -count=1 -timeout 20m -run TestAccLive ./internal/provider/
```

`make test-acceptance-live` runs the same tests in Docker, with the Terraform
version pinned in the `Dockerfile`. It takes both variables from your
environment.

## Required checks

Pull requests into `main` must pass these 10 checks:

| Check | What it does |
|-------|--------------|
| `ci / Build & vet` | `go build`, `go mod tidy` is a no-op, README version table is current |
| `ci / golangci-lint` | golangci-lint with `.golangci.yml` (includes vet and gofmt) |
| `ci / Workflow lint` | actionlint and zizmor over the workflows |
| `ci / Unit tests` | `go test -race ./...` with coverage |
| `ci / Acceptance tests` | `TF_ACC=1` tests against the local API mock |
| `ci / Release snapshot` | goreleaser snapshot build, then checks the artifact set |
| `Secret detection` | gitleaks |
| `govulncheck` | known vulnerabilities in reachable Go code |
| `Trivy (vulnerabilities, misconfig, secrets)` | fails on high or critical findings |
| `Dependency review` | fails on newly added dependencies with high or critical advisories |

Other checks are informational and don't block a merge:

- **Tool pins are current** (`scripts/check-pins.sh`) fails when a tool pinned
  outside a Dependabot-managed manifest falls behind upstream. An upstream
  release is not a problem with your change, so ignore it unless your PR touches
  those pins.
- **CodeQL** runs on pushes to `main` and weekly, not on pull requests.

The `main` ruleset requires no approving review, since there is only one
maintainer. Merging is still the maintainer's call.

## Pull requests

- One change per pull request.
- Behaviour changes come with tests. A resource change usually needs a unit
  test and an acceptance test step against the mock.
- Schema changes are reflected in `README.md` and `docs/`.

## Releases

Releases are cut by the maintainer by pushing a `v*` tag. The release workflow
runs the same CI as pull requests against the tagged commit before goreleaser
builds and signs the artifacts.
