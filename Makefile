.PHONY: test test-unit test-integration test-cover test-cover-html web-test-cover build run docker-up docker-down lint lint-fix vulncheck web-install web-dev web-build web-test web-e2e build-with-ui dev e2e-up e2e-down e2e-seed

test: test-unit

test-unit:
	go test ./...

test-integration:
	go test -tags integration ./...

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

build-with-ui: web-build build

dev: ## Start all services (Docker + Go API + Vite HMR)
	@./scripts/dev.sh
