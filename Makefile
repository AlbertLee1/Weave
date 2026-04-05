.PHONY: test test-unit test-integration build run docker-up docker-down lint web-install web-dev web-build web-test web-e2e build-with-ui dev

test: test-unit

test-unit:
	go test ./...

test-integration:
	go test -tags integration ./...

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

build-with-ui: web-build build

dev: ## Start all services (Docker + Go API + Vite HMR)
	@./scripts/dev.sh
