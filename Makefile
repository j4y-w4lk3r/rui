# Local build/install with git-derived version metadata.
# Release builds use goreleaser (.goreleaser.yaml) on git tag push.

BINARY := rui

# Nearest tag + commits since (e.g. v0.1.0-1-gd15b7ec); strip leading v.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

# Where `make install` puts the binary. Override: make install INSTALL_DIR=/usr/local/bin
INSTALL_DIR ?= $(HOME)/.local/bin

.PHONY: build install version test vet release-snapshot clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/rui

install:
	@mkdir -p "$(INSTALL_DIR)"
	GOBIN="$(INSTALL_DIR)" go install -ldflags "$(LDFLAGS)" ./cmd/rui
	@rm -f "$$(go env GOPATH)/bin/rui" 2>/dev/null || true
	@echo "→ $(INSTALL_DIR)/rui ($$( "$(INSTALL_DIR)/rui" -version ))"
	@echo ""
	@echo "If plain 'rui' still runs an old build, refresh the shell hash table:"
	@echo "  rehash          # zsh"
	@echo "  hash -r         # bash"

version:
	@echo "version=$(VERSION) commit=$(COMMIT) date=$(DATE)"

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

# Dry-run a goreleaser release (binaries land in dist/, nothing published).
release-snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -f $(BINARY)
