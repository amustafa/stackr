BINARY   := sr
BIN_DIR  := build
LINK_DIR ?= $(STACKR_LINK_DIR)

.PHONY: build install test clean link setup

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) .

install:
	go install .

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR)

link: build
	@if [ -z "$(LINK_DIR)" ]; then \
		echo "STACKR_LINK_DIR is not set." >&2; \
		exit 1; \
	fi
	@mkdir -p $(LINK_DIR)
	ln -sf $(CURDIR)/$(BIN_DIR)/$(BINARY) $(LINK_DIR)/$(BINARY)

setup:
	@if [ ! -f .envrc ]; then \
		cp .envrc.example .envrc; \
		echo "Created .envrc from .envrc.example"; \
	fi
	@command -v direnv >/dev/null 2>&1 && direnv allow || echo "Run 'source .envrc' to load environment"
