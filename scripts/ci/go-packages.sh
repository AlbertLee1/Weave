#!/usr/bin/env bash
set -euo pipefail

list_packages() {
	case "${1:-}" in
		"" | "--test")
			go list -e ./...
			;;
		"--build")
			go list -e -f '{{if or .GoFiles .CgoFiles}}{{.ImportPath}}{{end}}' ./...
			;;
		"--acceptance-data")
			go list -e ./test/chinook ./test/northwind
			;;
		*)
			echo "usage: $0 [--test|--build|--acceptance-data]" >&2
			exit 2
			;;
	esac
}

# `go list ./...` traverses installed web dependencies when web/node_modules
# exists locally. Keep all-package Go gates scoped to repository packages.
#
# The Chinook and Northwind suites are full data acceptance checks. Keep the
# default unit/race/coverage package list fast; run them explicitly through
# `--acceptance-data` or `make test-data-acceptance`.
case "${1:-}" in
	"--acceptance-data")
		list_packages "$1" | awk 'NF && $0 !~ /(^|\/)node_modules(\/|$)/ { print }'
		;;
	*)
		list_packages "${1:-}" | awk 'NF && $0 !~ /(^|\/)node_modules(\/|$)/ && $0 !~ /\/test\/(chinook|northwind)$/ { print }'
		;;
esac
