.PHONY: build run test clean frontend deps embed-frontend

BINARY=bin/backupmanager
GO=go

build: deps frontend embed-frontend
	$(GO) build -o $(BINARY) ./cmd/server

deps:
	@echo "Checking system dependencies..."
	@which lftp > /dev/null 2>&1 || (echo "Installing lftp..." && sudo apt-get install -y lftp 2>/dev/null || brew install lftp 2>/dev/null || echo "WARNING: lftp not installed — FTPS backup will not work")
	@which sshpass > /dev/null 2>&1 || (echo "Installing sshpass..." && sudo apt-get install -y sshpass 2>/dev/null || brew install sshpass 2>/dev/null || echo "WARNING: sshpass not installed — SSH password auth will not work")
	@which rsync > /dev/null 2>&1 || (echo "Installing rsync..." && sudo apt-get install -y rsync 2>/dev/null || echo "WARNING: rsync not installed — backup will not work")

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
