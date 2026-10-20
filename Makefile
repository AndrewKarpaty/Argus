BINDIR  := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build release run ui ui-dev ui-install ui-test ui-clean test test-race cover vet fmt tidy lint clean docker demo

all: fmt vet test build

build: ## Build both binaries into ./bin (embeds whatever is in web/dist)
	@mkdir -p $(BINDIR)
	go build -ldflags "$(LDFLAGS)" -o $(BINDIR)/argusd ./cmd/argusd
	go build -ldflags "$(LDFLAGS)" -o $(BINDIR)/argusctl ./cmd/argusctl

release: ui build ## Build the console, then the binaries with it embedded

ui: ## Build the web console into web/dist
	cd web && npm ci && npm run build

ui-install: ## Install the console's dependencies
	cd web && npm ci

ui-test: ## Typecheck the console and run the graph-layout checks
	cd web && npm test

ui-dev: ## Run the console with hot reload against a local argusd on :8080
	cd web && npm run dev

ui-clean:
	rm -rf web/dist web/node_modules
	@mkdir -p web/dist && touch web/dist/.gitkeep

run: ## Run the API server
	go run -ldflags "$(LDFLAGS)" ./cmd/argusd

demo: ## Investigate a two-service incident from the command line
	ARGUS_ENGINE=heuristic go run ./cmd/argusctl investigate -service checkout -service orders

test: ## Run the test suite
	go test ./...

test-race: ## Run the test suite with the race detector
	go test -race ./...

cover: ## Write and open an HTML coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "report: coverage.html"

vet: ## Run go vet
	go vet ./...

fmt: ## Format the tree
	gofmt -l -w .

tidy: ## Tidy and verify module dependencies
	go mod tidy
	go mod verify

lint: ## Run golangci-lint if it is installed
	@command -v golangci-lint >/dev/null 2>&1 \
		&& golangci-lint run \
		|| echo "golangci-lint not installed, skipping (brew install golangci-lint)"

docker: ## Build the container image
	docker build -t argus:$(VERSION) .

clean:
	rm -rf $(BINDIR) coverage.out coverage.html
