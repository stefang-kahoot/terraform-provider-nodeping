#!/bin/sh
# Fails when a tool pinned in two places disagrees with itself.
#
# goreleaser is pinned twice on purpose: ci.yml's snapshot job exists to prove
# the exact version release.yml will run, so the two must never differ.
#
# golangci-lint is pinned twice too: CI must lint with what `make lint` runs, or
# a clean local run can still fail the PR.
#
#   ./scripts/check-pins.sh
set -eu

cd "$(dirname "$0")/.."

CI=.github/workflows/ci.yml
RELEASE=.github/workflows/release.yml

fail() { echo "error: $*" >&2; exit 1; }

# pin <name> <file> <sed script>: prints the one pin the script finds in file.
pin() {
	value=$(sed -n "$3" "$2")
	[ -n "$value" ] || fail "could not find the $1 pin in $2"
	case $value in *"
"*) fail "found more than one $1 pin in $2" ;; esac
	printf '%s\n' "$value"
}

goreleaser_version='s/^ *version: *\(v[0-9][0-9.]*\)$/\1/p'
goreleaser_ci=$(pin goreleaser "$CI" "$goreleaser_version")
goreleaser_release=$(pin goreleaser "$RELEASE" "$goreleaser_version")

golangci_lint_ci=$(pin golangci-lint "$CI" 's/^  GOLANGCI_LINT_VERSION: *//p')
golangci_lint_make=$(pin golangci-lint Makefile 's/^GOLANGCI_LINT_VERSION := *//p')

[ "$goreleaser_ci" = "$goreleaser_release" ] ||
	fail "goreleaser is $goreleaser_release in release.yml but $goreleaser_ci in ci.yml"
[ "$golangci_lint_ci" = "$golangci_lint_make" ] ||
	fail "golangci-lint is $golangci_lint_make in the Makefile but $golangci_lint_ci in ci.yml"

echo "ok     goreleaser $goreleaser_ci"
echo "ok     golangci-lint $golangci_lint_ci"
