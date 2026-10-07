## What and why

<!-- What does this change, and what problem does it solve? Link the issue if there is one. -->

## How it was tested

<!-- Unit tests, acceptance tests against the mock, a manual `terraform apply`... -->

## Checklist

- [ ] `go test ./...` and `TF_ACC=1 go test -run TestAcc ./...` pass locally
- [ ] `make lint` is clean
- [ ] Schema changes are reflected in `README.md` and `docs/`
- [ ] Dependency bumps: `./scripts/sync-readme-versions.sh --check` passes (`make docs` regenerates the table)

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the checks CI requires.
