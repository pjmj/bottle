# Convenience targets. Run `make help` to list them.
.PHONY: help build test race lint fmt run web docker

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-10s %s\n", $$1, $$2}'

build: ## Build both binaries into ./bin
	go build -o bin/ ./cmd/...

test: ## Run all Go tests
	go test ./...

race: ## Run Go tests with the race detector (needs cgo)
	go test -race ./...

lint: ## Run golangci-lint
	golangci-lint run

fmt: ## Format Go code
	gofmt -w .

run: ## Run the API server
	go run ./cmd/server

web: ## Run the frontend dev server
	cd web && npm run dev

docker: ## Build and run the whole stack in Docker
	docker compose up --build
