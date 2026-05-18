.PHONY: test test-unit test-integration test-integration-phase6 test-integration-phase7 test-bdd test-cover test-cover-html test-cover-check test-cover-update test-contract web-test-cover build run docker-up docker-down lint lint-fix vulncheck web-install web-dev web-build web-test web-e2e build-with-ui dev e2e-up e2e-down e2e-seed test-parity bench bench-update pact-broker-up pact-broker-down pact-publish pact-list help

test: test-unit

help: ## Show available targets and clarify build variants
	@awk 'BEGIN { FS = ":.*##"; print "Available targets:" } /^[a-zA-Z0-9_.-]+:.*##/ { printf "  %-24s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

test-unit:
	go test $$(./scripts/ci/go-packages.sh)

test-integration:
	go test -tags integration ./...

test-contract: ## Run Pact-style consumer-driven contract tests (US-362, US-445)
	go test ./pkg/contract/... ./cmd/weave-pact/... -count=1
	go test ./cmd/server/ -run TestContract -count=1

# US-445: Pact Broker integration. Targets are opt-in via the docker-compose
# `pact` profile so the default `make docker-up` flow stays lean. Set
# WEAVE_PACT_BROKER_URL / WEAVE_PACT_BROKER_AUTH / WEAVE_PACT_VERSION before
# `make pact-publish` to override the local-broker defaults; the CI
# integration job typically points these at an external broker URL.
PACT_BROKER_URL ?= http://localhost:9292
PACT_BROKER_AUTH ?= Basic cGFjdGJyb2tlcjpwYWN0YnJva2Vy
PACT_VERSION ?= dev

pact-broker-up: ## Start the local Pact Broker (docker compose --profile pact)
	docker compose --profile pact up -d pact-broker-postgres pact-broker

pact-broker-down: ## Stop the local Pact Broker
	docker compose --profile pact down

pact-publish: ## Publish every cmd/server/testdata/pacts/*.pact.json to the broker
	go run ./cmd/weave-pact publish \
		-broker $(PACT_BROKER_URL) \
		-auth "$(PACT_BROKER_AUTH)" \
		-dir cmd/server/testdata/pacts \
		-version $(PACT_VERSION)

pact-list: ## List the latest pacts the broker holds for the weave-server provider
	go run ./cmd/weave-pact list \
		-broker $(PACT_BROKER_URL) \
		-auth "$(PACT_BROKER_AUTH)" \
		-provider weave-server

BENCH_TIME ?= 200ms

bench: ## Run the US-441 perf regression suite and gate the result against bench/baseline.json
	go test -bench='Benchmark.*_US441' -benchtime=$(BENCH_TIME) -run='^$$' ./bench/... 2>&1 \
		| go run ./cmd/benchcheck -baseline bench/baseline.json -output bench/results.json

bench-update: ## Re-record bench/baseline.json from a fresh run (use only when an intentional perf change is made)
	go test -bench='Benchmark.*_US441' -benchtime=$(BENCH_TIME) -run='^$$' ./bench/... 2>&1 \
		| go run ./cmd/benchcheck -update bench/baseline.json

test-integration-phase6:
	go test -tags integration ./test/integration/phase6/... -v

test-integration-phase7:
	go test -tags integration ./test/integration/phase7/... -v

test-bdd: ## Run the godog Cucumber BDD suite (test/bdd, requires Docker for testcontainers)
	go test -tags bdd -count=1 -v ./test/bdd/...

# US-056 / PC-C13: cover-profile target is shared by the local `test-cover`
# convenience target and the CI gate (`test-cover-check`). Scoping to
# ./pkg/... keeps the run fast enough for PR feedback and matches the
# packages covered by the floor thresholds in coverage/thresholds.json.
COVER_PKG ?= ./pkg/...

test-cover: ## Run tests with cover-profile and print per-package + total summary
	go test -race -coverprofile=coverage.out -covermode=atomic $(COVER_PKG)
	@echo "----- per-package coverage -----"
	@go tool cover -func=coverage.out | awk '$$1 !~ /\.go:/ { next } { sub(/\/[^\/]+\.go.*$$/, "", $$1); pct=$$NF; gsub(/%/, "", pct); sum[$$1]+=pct; n[$$1]++ } END { for (p in sum) printf "%-60s %6.1f%%\n", p, sum[p]/n[p] }' | sort
	@echo "----- coverage summary -----"
	@go tool cover -func=coverage.out | grep -E '^total:'

test-cover-html: test-cover
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

test-cover-check: ## US-056: run cover-profile + enforce coverage/thresholds.json + coverage/baseline.json regression gate
	go test -race -coverprofile=coverage.out -covermode=atomic $(COVER_PKG)
	@go run ./cmd/covercheck \
		-profile coverage.out \
		-thresholds coverage/thresholds.json \
		-baseline coverage/baseline.json \
		-md coverage/report.md \
		-output coverage/report.json

test-cover-update: ## US-056: re-record coverage/baseline.json from the current run (commit alongside intentional coverage shifts)
	go test -race -coverprofile=coverage.out -covermode=atomic $(COVER_PKG)
	@go run ./cmd/covercheck -profile coverage.out -update coverage/baseline.json
	@echo "baseline updated; review coverage/baseline.json before committing"

vulncheck:
	@./scripts/ci/govulncheck.sh

lint-fix:
	golangci-lint run --fix ./...

web-test-cover:
	cd web && npx vitest run --coverage

build: ## Build Go server without generating embedded WebUI assets
	go build -o bin/weave ./cmd/server

run: build
	./bin/weave

docker-up:
	docker compose up -d

docker-down:
	docker compose down

lint:
	golangci-lint run ./...

web-install:
	cd web && npm install

web-dev:
	cd web && npm run dev

web-build: ## Build WebUI assets and copy them into the server embed tree
	cd web && npm run build
	rm -rf cmd/server/web/dist
	mkdir -p cmd/server/web
	cp -r web/dist cmd/server/web/dist

web-test:
	cd web && npm test

web-e2e:
	cd web && npx playwright test

e2e-up: ## Bring up the full stack for Playwright E2E (idempotent)
	@./scripts/e2e-setup.sh

e2e-down: ## Stop the Playwright E2E stack (idempotent)
	@./scripts/e2e-teardown.sh

e2e-seed: ## Wipe + reseed Northwind + test users for Playwright
	@./test/fixtures/e2e_seed.sh

test-parity: ## Run the Foundry parity runner (starts the E2E stack if not already up)
	@if ! curl -fsS http://localhost:9117/health >/dev/null 2>&1; then \
		echo "[test-parity] weave not reachable at :9117, bringing up the E2E stack"; \
		./scripts/e2e-setup.sh; \
	fi
	@go run ./test/foundry_parity -v

build-with-ui: web-build build ## Build production server with embedded WebUI assets

dev: ## Start all services (Docker + Go API + Vite HMR)
	@./scripts/dev.sh
