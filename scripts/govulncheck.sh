#!/bin/sh
# Runs govulncheck from backend/ and fails on any vulnerability this code
# reaches, except the ones excused below.
#
# govulncheck has no ignore flag, so this reads its JSON: a finding whose
# trace names a function is one the code reaches (what the plain text run
# fails on). Module- and package-level findings are reported but don't fail,
# as before. The version is pinned because the script depends on that JSON
# shape; bump it on purpose.
set -eu

# Run from backend/ of this script's own checkout, wherever this was started.
cd "$(dirname "$0")/../backend"

govulncheck=golang.org/x/vuln/cmd/govulncheck@v1.9.0

# One advisory per line: the OSV id, then why it's excused. An excuse states
# why the code is safe, not just that the scan is noisy. When an excused id
# stops being found, the script says so without failing: the vulnerability
# database changes under every branch at once, so failing on it would turn
# unrelated PRs red. Delete the entry then.
excused='
GO-2026-6875 echo/v4 reverse proxy forwarding X-Real-IP. Fixed in v4.16.3 (GHSA-99jh-6h7p-pp36, Echo CHANGELOG), which go.mod requires, but OSV still lists no fixed v4 version. Helmcentral uses no echo proxy middleware either.
GO-2026-6872 echo/v4 rate limiter trusting X-Forwarded-For. Fixed in v4.16.3 (GHSA-246p-cpwv-v3jq), which go.mod requires, but OSV still lists no fixed v4 version. Helmcentral uses no echo rate limiter, and sets its own IPExtractor (client_ip.go).
'

command -v jq >/dev/null || { echo "govulncheck: jq is required" >&2; exit 1; }

out=$(mktemp)
trap 'rm -f "$out"' EXIT

go run "$govulncheck" -format json ./... >"$out"

# jq on its own so a failure stops the script rather than leaving the list empty.
reached_raw=$(jq -r 'select(.finding.trace[0].function? // empty) | .finding.osv' "$out")
reached=$(printf '%s\n' "$reached_raw" | awk 'NF' | sort -u)
excused_ids=$(printf '%s\n' "$excused" | awk 'NF { print $1 }' | sort -u)

failed=0
for id in $reached; do
	if printf '%s\n' "$excused_ids" | grep -qx "$id"; then
		echo "govulncheck: $id excused: $(printf '%s\n' "$excused" | grep "^$id " | cut -d' ' -f2-)"
	else
		echo "govulncheck: $id is reachable: https://pkg.go.dev/vuln/$id" >&2
		failed=1
	fi
done
for id in $excused_ids; do
	if ! printf '%s\n' "$reached" | grep -qx "$id"; then
		echo "govulncheck: $id is excused but no longer found. Remove its entry from scripts/govulncheck.sh."
	fi
done

if [ "$failed" -ne 0 ]; then
	echo "govulncheck: run 'go run $govulncheck ./...' in backend/ for the traces." >&2
	exit 1
fi
echo "govulncheck: no reachable vulnerabilities beyond the excused ones"
