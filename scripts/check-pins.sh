#!/bin/sh
# Fails when a tool version pinned outside any manifest Dependabot reads has
# fallen behind upstream.
#
# Dependabot covers go.mod, Dockerfile `FROM` lines and workflow `uses:`. The
# pins below live in `env:` values, action inputs, a `go install` and the
# Makefile, so no bot will ever bump them. This script turns that drift into a
# failing check instead, the same way sync-readme-versions.sh does for the
# README table.
#
# A release younger than COOLDOWN_DAYS is reported but not flagged, matching the
# Dependabot cooldown: a compromised release is usually yanked within days.
#
#   ./scripts/check-pins.sh
#
# Needs curl and jq. Set GITHUB_TOKEN to lift the GitHub API rate limit.
set -eu

cd "$(dirname "$0")/.."

COOLDOWN_DAYS=${COOLDOWN_DAYS:-7}
SECURITY=.github/workflows/security.yml

fail() { echo "error: $*" >&2; exit 2; }

# --- collect the pins -------------------------------------------------------
gitleaks=$(sed -n 's/^  GITLEAKS_VERSION: *//p' "$SECURITY")
trivy=$(sed -n 's/^  TRIVY_VERSION: *//p' "$SECURITY")
govulncheck=$(sed -n 's|.*govulncheck@\(v[0-9][0-9.]*\).*|\1|p' "$SECURITY")
golangci_lint=$(sed -n 's/^GOLANGCI_LINT_VERSION := *//p' Makefile)

# goreleaser is pinned twice on purpose: ci.yml's snapshot job exists to prove
# the exact version release.yml will run, so the two must never differ.
goreleaser_ci=$(sed -n 's/^ *version: *\(v[0-9][0-9.]*\)$/\1/p' .github/workflows/ci.yml)
goreleaser=$(sed -n 's/^ *version: *\(v[0-9][0-9.]*\)$/\1/p' .github/workflows/release.yml)

for pair in "gitleaks:$gitleaks" "trivy:$trivy" "govulncheck:$govulncheck" \
	"golangci_lint:$golangci_lint" "goreleaser:$goreleaser" \
	"goreleaser_ci:$goreleaser_ci"; do
	name=${pair%%:*}
	value=${pair#*:}
	[ -n "$value" ] || fail "could not determine the $name pin"
	case $value in *"
"*) fail "found more than one $name pin" ;; esac
done

[ "$goreleaser" = "$goreleaser_ci" ] ||
	fail "goreleaser is $goreleaser in release.yml but $goreleaser_ci in ci.yml"

# --- look up upstream ---------------------------------------------------------
# Each lookup prints "<version> <age in days>".
github_latest() {
	if [ -n "${GITHUB_TOKEN:-}" ]; then
		body=$(curl -fsS -H "Authorization: Bearer $GITHUB_TOKEN" \
			"https://api.github.com/repos/$1/releases/latest")
	else
		body=$(curl -fsS "https://api.github.com/repos/$1/releases/latest")
	fi || fail "could not query the latest $1 release"
	printf '%s' "$body" | jq -r '"\(.tag_name) \((now - (.published_at | fromdateiso8601)) / 86400 | floor)"'
}

# golang/vuln publishes tags, not GitHub releases, so ask the Go module proxy.
go_latest() {
	body=$(curl -fsS "https://proxy.golang.org/$1/@latest") ||
		fail "could not query the latest $1 version"
	printf '%s' "$body" | jq -r '"\(.Version) \((now - (.Time | sub("\\.[0-9]+"; "") | fromdateiso8601)) / 86400 | floor)"'
}

stale=0

# check <name> <pinned> <"latest age"> <where the pin lives>
check() {
	latest=${3% *}
	age=${3#* }
	# A failed lookup leaves an empty string, since $(...) in an argument does
	# not trip `set -e`.
	case $age in '' | *[!0-9]*) fail "could not look up the latest $1" ;; esac
	if [ "${2#v}" = "${latest#v}" ]; then
		echo "ok     $1 $2"
	elif [ "$age" -lt "$COOLDOWN_DAYS" ]; then
		echo "wait   $1 $2 (latest $latest is $age days old, cooldown is $COOLDOWN_DAYS)"
	else
		echo "STALE  $1 $2 -> $latest (released $age days ago), in $4"
		stale=1
	fi
}

check gitleaks "$gitleaks" "$(github_latest gitleaks/gitleaks)" "$SECURITY"
check trivy "$trivy" "$(github_latest aquasecurity/trivy)" "$SECURITY"
check goreleaser "$goreleaser" "$(github_latest goreleaser/goreleaser)" \
	".github/workflows/ci.yml and release.yml"
check golangci-lint "$golangci_lint" "$(github_latest golangci/golangci-lint)" Makefile
check govulncheck "$govulncheck" "$(go_latest golang.org/x/vuln)" "$SECURITY"

exit "$stale"
