.PHONY: build run test clean frontend

BINARY=bin/backupmanager
GO=go

build: frontend embed-frontend
	$(GO) build -o $(BINARY) ./cmd/server

embed-frontend:
	rm -rf cmd/server/static
	cp -r frontend/dist cmd/server/static

run:
	$(GO) run ./cmd/server

test:
	$(GO) test ./cmd/... ./internal/... -v -cover

test-short:
	$(GO) test ./cmd/... ./internal/... -short -v

clean:
	rm -rf $(BINARY) data/

frontend:
	cd frontend && npm ci && npm run build

dev:
	$(GO) run ./cmd/server
