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
		*)
			echo "usage: $0 [--test|--build]" >&2
			exit 2
			;;
	esac
}

# `go list ./...` traverses installed web dependencies when web/node_modules
# exists locally. Keep all-package Go gates scoped to repository packages.
list_packages "${1:-}" | awk 'NF && $0 !~ /(^|\/)node_modules(\/|$)/ { print }'
