# ============================================================================
# Whisper — Anonymous Real-Time Chat Application
# ============================================================================

.DEFAULT_GOAL := help

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------
GO       := go
GOFLAGS  := CGO_ENABLED=0
LDFLAGS  := -ldflags="-s -w"
BIN_DIR  := bin

SERVICES := wsserver matcher moderator

# ---------------------------------------------------------------------------
# Go targets
# ---------------------------------------------------------------------------

.PHONY: build
build: build-wsserver build-matcher build-moderator ## Build all Go binaries into bin/

.PHONY: build-wsserver
build-wsserver: ## Build the WebSocket server
	@echo "Building wsserver..."
	@mkdir -p $(BIN_DIR)
	$(GOFLAGS) $(GO) build $(LDFLAGS) -o $(BIN_DIR)/wsserver ./cmd/wsserver

.PHONY: build-matcher
build-matcher: ## Build the matcher service
	@echo "Building matcher..."
	@mkdir -p $(BIN_DIR)
	$(GOFLAGS) $(GO) build $(LDFLAGS) -o $(BIN_DIR)/matcher ./cmd/matcher

.PHONY: build-moderator
build-moderator: ## Build the moderator service
	@echo "Building moderator..."
	@mkdir -p $(BIN_DIR)
	$(GOFLAGS) $(GO) build $(LDFLAGS) -o $(BIN_DIR)/moderator ./cmd/moderator

.PHONY: run
run: ## Run the WebSocket server (default service)
	$(GOFLAGS) $(GO) run ./cmd/wsserver

.PHONY: run-matcher
run-matcher: ## Run the matcher service
	$(GOFLAGS) $(GO) run ./cmd/matcher

.PHONY: run-moderator
run-moderator: ## Run the moderator service
	$(GOFLAGS) $(GO) run ./cmd/moderator

.PHONY: test
test: ## Run all Go tests with race detection
	$(GO) test -v -race ./...

.PHONY: lint
lint: ## Run go vet on all packages
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format all Go source files
	gofmt -w .

.PHONY: tidy
tidy: ## Tidy Go module dependencies
	$(GO) mod tidy

# ---------------------------------------------------------------------------
# Docker targets
# ---------------------------------------------------------------------------

.PHONY: docker-up
docker-up: ## Start infrastructure containers (Redis, NATS, PostgreSQL, HAProxy)
	docker compose up -d

.PHONY: docker-down
docker-down: ## Stop and remove infrastructure containers
	docker compose down

.PHONY: docker-logs
docker-logs: ## Follow infrastructure container logs
	docker compose logs -f

.PHONY: docker-ps
docker-ps: ## Show running infrastructure containers
	docker compose ps

# ---------------------------------------------------------------------------
# Frontend targets
# ---------------------------------------------------------------------------

.PHONY: fe-install
fe-install: ## Install frontend dependencies
	cd frontend && npm install

.PHONY: fe-dev
fe-dev: ## Start frontend dev server
	cd frontend && npm run dev

.PHONY: fe-build
fe-build: ## Build frontend for production
	cd frontend && npm run build

# ---------------------------------------------------------------------------
# Utility targets
# ---------------------------------------------------------------------------

.PHONY: clean
clean: ## Remove build artifacts (bin/ and frontend/build/)
	@echo "Cleaning build artifacts..."
	@rm -rf $(BIN_DIR)
	@rm -rf frontend/build

.PHONY: all
all: build fe-build ## Build Go binaries and frontend

.PHONY: help
help: ## Print this help message
	@printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
	@echo ""
