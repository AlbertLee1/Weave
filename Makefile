.PHONY: test test-unit test-integration test-integration-phase6 test-integration-phase7 test-cover test-cover-html test-contract web-test-cover build run docker-up docker-down lint lint-fix vulncheck web-install web-dev web-build web-test web-e2e build-with-ui dev e2e-up e2e-down e2e-seed test-parity bench bench-update

test: test-unit

test-unit:
	go test ./...

test-integration:
	go test -tags integration ./...

test-contract: ## Run Pact-style consumer-driven contract tests (US-362)
	go test ./pkg/contract/... -count=1
	go test ./cmd/server/ -run TestContract -count=1

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

test-cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "----- coverage summary -----"
	@go tool cover -func=coverage.out | grep -E '^total:'

test-cover-html: test-cover
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

vulncheck:
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

lint-fix:
	golangci-lint run --fix ./...

web-test-cover:
	cd web && npx vitest run --coverage

build:
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

web-build:
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

build-with-ui: web-build build

dev: ## Start all services (Docker + Go API + Vite HMR)
	@./scripts/dev.sh
