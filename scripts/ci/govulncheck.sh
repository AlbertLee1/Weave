#!/usr/bin/env bash
set -euo pipefail

govulncheck_bin=$(command -v govulncheck || true)
if [[ -z "$govulncheck_bin" ]]; then
	go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck_bin="$(go env GOPATH)/bin/govulncheck"
fi

# Keep the PR security gate actionable: scan production/source packages while
# excluding package-only integration-test helpers that import Docker APIs for
# local testcontainers setup. Those helpers are not linked into Weave services
# or CLIs, and current Docker/Moby findings have no fixed module version.
packages=$(go list ./... | grep -Ev '/internal/testutil$')

"$govulncheck_bin" $packages
