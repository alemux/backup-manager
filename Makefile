.PHONY: build run test clean frontend

BINARY=bin/backupmanager
GO=go

build: frontend
	$(GO) build -o $(BINARY) ./cmd/server

run:
	$(GO) run ./cmd/server

test:
	$(GO) test ./... -v -cover

test-short:
	$(GO) test ./... -short -v

clean:
	rm -rf $(BINARY) data/

frontend:
	cd frontend && npm ci && npm run build

dev:
	$(GO) run ./cmd/server
