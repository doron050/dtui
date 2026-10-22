.PHONY: all build install test test-coverage vet lint fmt clean

# Leading "v" stripped so local builds report the same shape as release builds.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# dtui stamps its own version plus dtctl's pkg/version vars, which feed
# code paths in the consumed dtctl packages that read the build version.
DTCTL_VERSION_PKG = github.com/dynatrace-oss/dtctl/pkg/version
LDFLAGS = -ldflags "-X main.version=$(VERSION) -X $(DTCTL_VERSION_PKG).Version=$(VERSION) -X $(DTCTL_VERSION_PKG).Commit=$(COMMIT) -X $(DTCTL_VERSION_PKG).Date=$(DATE) -s -w"
INSTALL_BIN_DIR ?= $(if $(GOBIN),$(GOBIN),$(shell go env GOPATH)/bin)

all: build

# Build the dtui binary
build:
	@echo "Building dtui $(VERSION)..."
	@go build $(LDFLAGS) -o bin/dtui .

# Install dtui plus the dtctl-tui plugin alias, so `dtctl tui` dispatches
# to it via dtctl's exec-plugin convention.
install:
	@echo "Installing dtui to $(INSTALL_BIN_DIR)..."
	@mkdir -p "$(INSTALL_BIN_DIR)"
	@go build $(LDFLAGS) -o "$(INSTALL_BIN_DIR)/dtui" .
	@ln -sf "$(INSTALL_BIN_DIR)/dtui" "$(INSTALL_BIN_DIR)/dtctl-tui"
	@echo "Installed dtui and the dtctl-tui plugin alias."
	@case ":$$PATH:" in \
	  *:"$(INSTALL_BIN_DIR)":*) ;; \
	  *) echo ""; \
	     echo "  NOTE: $(INSTALL_BIN_DIR) is not in your PATH."; \
	     echo "  Add the following line to your shell profile (~/.bashrc, ~/.zshrc, etc.):"; \
	     echo "    export PATH=\"\$$PATH:\$$(go env GOPATH)/bin\""; \
	     echo "  Then run: source ~/.bashrc  (or open a new terminal)"; \
	     echo "" ;; \
	esac

test:
	@go test -race ./...

test-coverage:
	@go test -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1

vet:
	@go vet ./...

lint:
	@golangci-lint run ./...

fmt:
	@golangci-lint fmt ./...

clean:
	@rm -rf bin coverage.out
