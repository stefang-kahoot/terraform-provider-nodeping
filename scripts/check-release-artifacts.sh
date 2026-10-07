#!/bin/sh
# Asserts that a goreleaser run produced the artifact set the Terraform and
# OpenTofu registries need. CI runs it against a snapshot build.
#
# A snapshot never writes the registry manifest into dist/ (release.extra_files
# only materialise on publish), so the manifest is checked through its line in
# SHA256SUMS. Whether it actually gets attached to the GitHub release is the one
# thing this cannot see.
#
#   ./scripts/check-release-artifacts.sh [dist-dir]
set -eu

cd "$(dirname "$0")/.."

DIST=${1:-dist}
MANIFEST=terraform-registry-manifest.json
PLATFORMS='darwin_amd64 darwin_arm64 linux_amd64 linux_arm64 windows_amd64 windows_arm64'

fail() { echo "error: $*" >&2; exit 1; }

set -- "$DIST"/*_SHA256SUMS
[ "$#" -eq 1 ] && [ -f "$1" ] || fail "expected exactly one *_SHA256SUMS in $DIST"
sums=$1

for p in $PLATFORMS; do
	grep -q "_${p}\.zip\$" "$sums" || fail "$sums has no ${p} zip"
done

want=$(sha256sum "$MANIFEST" | cut -d' ' -f1)
got=$(awk '/_manifest\.json$/ { print $1 }' "$sums")
[ -n "$got" ] || fail "$sums does not cover the registry manifest"
[ "$got" = "$want" ] || fail "$sums manifest hash $got does not match $MANIFEST ($want)"

# The plugin framework serves protocol 6; without this the registries assume 5.
jq -e '.metadata.protocol_versions == ["6.0"]' "$MANIFEST" >/dev/null ||
	fail "$MANIFEST does not declare protocol_versions [\"6.0\"]"

echo "release artifacts OK: $(wc -l <"$sums" | tr -d ' ') checksummed files"
