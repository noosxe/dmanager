# Launch control for the docker compose stack (docker-compose.yml + optional .env).

COMPOSE := docker compose -f docker-compose.yml


# Recipient address for the smtp-test target (make smtp-test TO=you@example.com)
TO ?=
.PHONY: help
help:
	@echo "Targets:"
	@echo "  launch   start the stack in the background"
	@echo "  stop     stop and remove the stack"
	@echo "  restart  stop, then launch"
	@echo "  build    build the local image (compose build: section)"
	@echo "  pull     pull the images referenced by the stack"
	@echo "  logs     follow stack logs"
	@echo "  status   show container status"
	@echo "  smtp-test send a test email via the relay (requires TO=you@example.com)"
	@echo "  lint     run golangci-lint (same config as CI)"
	@echo "  lint-install install the CI-pinned golangci-lint binary"

.PHONY: launch
launch:
	$(COMPOSE) up -d

.PHONY: stop
stop:
	$(COMPOSE) down

.PHONY: restart
restart: stop launch
	@echo "done"

.PHONY: build
build:
	$(COMPOSE) build


# Full rebuild bypassing the BuildKit layer and context caches — the escape
# hatch for the stat-cache pathology where --build serves a stale binary
# despite changed sources (seen with host/daemon clock skew).
.PHONY: build-no-cache
build-no-cache:
	$(COMPOSE) build --no-cache
.PHONY: pull
pull:
	$(COMPOSE) pull

.PHONY: logs
logs:
	$(COMPOSE) logs -f

.PHONY: status
status:
	$(COMPOSE) ps

.PHONY: smtp-test
smtp-test:
ifndef TO
	$(error usage: make smtp-test TO=you@example.com)
endif
	$(COMPOSE) exec dmanager dmanager smtp test --to=$(TO)

# golangci-lint must match the CI pin (.github/workflows/backend.yml). An older
# binary cannot read Go 1.27 stdlib export data — the local-lint breakage was a
# stale linter, not a toolchain incompatibility (issue #311).
GOLANGCI_LINT_VERSION ?= v2.13.1
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo "$$(go env GOPATH)/bin/golangci-lint")

.PHONY: lint-install
lint-install:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b "$$(go env GOPATH)/bin" $(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint:
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then \
		echo "golangci-lint not found — install the CI-pinned build with:"; \
		echo "  make lint-install"; \
		exit 1; \
	fi
	@if ! $(GOLANGCI_LINT) version 2>/dev/null | grep -qF "$(GOLANGCI_LINT_VERSION:v%=%)"; then \
		echo "warning: $$( $(GOLANGCI_LINT) version 2>/dev/null | head -1 )"; \
		echo "warning: CI pins $(GOLANGCI_LINT_VERSION) — run 'make lint-install' for parity"; \
	fi
	$(GOLANGCI_LINT) run
