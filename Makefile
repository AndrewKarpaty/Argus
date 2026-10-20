BINDIR  := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build run cli test test-race cover vet fmt tidy lint clean docker demo

all: fmt vet test build

build: ## Build both binaries into ./bin
	@mkdir -p $(BINDIR)
	go build -ldflags "$(LDFLAGS)" -o $(BINDIR)/argusd ./cmd/argusd
	go build -ldflags "$(LDFLAGS)" -o $(BINDIR)/argusctl ./cmd/argusctl

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
