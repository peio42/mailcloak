BINARY := mailcloak
BIN_DIR := bin

.PHONY: build venv run test test-e2e tidy clean install

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(BINARY) ./cmd/$(BINARY)

venv:
	python -m venv .venv
	.venv/bin/pip install -r requirements.txt
	.venv/bin/pip install ruff

run:
	go run ./cmd/$(BINARY)

test:
	gofmt -w .
	go vet ./...
	go test -race ./...
	go test -tags=integration ./...
	python -m compileall mailcloakctl
	ruff check --fix mailcloakctl
	ruff format mailcloakctl

test-e2e:
	go test -tags=e2e ./tests/e2e -v

tidy:
	go mod tidy

clean:
	rm -f $(BIN_DIR)/$(BINARY)

install: build
	sudo install -m 0755 $(BIN_DIR)/$(BINARY) /usr/local/sbin/$(BINARY)
	sudo install -m 0755 mailcloakctl /usr/local/sbin/mailcloakctl

# --- E2E env selection --------------------------------------------------------

# Usage:
#   make e2e-up IDP=keycloak
#   make e2e-down IDP=authentik
#   make e2e-up-keycloak
#   make e2e-down-authentik

IDP ?= keycloak

E2E_PROJECT := e2e-$(IDP)
E2E_DIR := tests/e2e
E2E_BASE := $(E2E_DIR)/docker-compose.base.yml
E2E_IDP  := $(E2E_DIR)/docker-compose.$(IDP).yml

# Validate IDP early with a friendly error
VALID_IDPS := keycloak authentik
ifeq (,$(filter $(IDP),$(VALID_IDPS)))
$(error Invalid IDP: "$(IDP)". Possible values: $(VALID_IDPS))
endif

# Compose command assembled once
DC_E2E = docker compose \
	-f $(E2E_BASE) \
	-f $(E2E_IDP) \
	-p $(E2E_PROJECT)

.PHONY: e2e-up e2e-down e2e-logs e2e-ps e2e-restart e2e-seed e2e-reset

e2e-up: ## Start the e2e environment (IDP=keycloak|authentik)
	$(DC_E2E) up -d --build

e2e-down: ## Stop and remove the e2e environment (IDP=keycloak|authentik)
	$(DC_E2E) down

e2e-logs: ## Follow the logs of the e2e environment
	$(DC_E2E) logs -f --tail=200

e2e-ps: ## View the status of the e2e environment containers
	$(DC_E2E) ps

e2e-restart: e2e-down e2e-up ## Restart the e2e environment cleanly

e2e-seed: ## Seed users/data for the e2e environment (IDP=keycloak|authentik)
ifeq ($(IDP),authentik)
	$(DC_E2E) exec -T authentik-worker /providers/authentik/bootstrap.sh
else ifeq ($(IDP),keycloak)
	@echo "Recreating keycloak to re-import realm fixture..."
	$(DC_E2E) up -d --force-recreate keycloak
else
	$(error Unsupported IDP: $(IDP))
endif

e2e-reset: ## Fully reset e2e environment (down -v, up, seed) for IDP=keycloak|authentik
	$(DC_E2E) down -v
	$(MAKE) e2e-up IDP=$(IDP)
	$(MAKE) e2e-seed IDP=$(IDP)

# --- Friendly aliases ---------------------------------------------------------

.PHONY: e2e-up-keycloak e2e-down-keycloak e2e-up-authentik e2e-down-authentik
.PHONY: e2e-seed-keycloak e2e-seed-authentik
.PHONY: e2e-reset-keycloak e2e-reset-authentik

e2e-up-keycloak:
	$(MAKE) e2e-up IDP=keycloak

e2e-down-keycloak:
	$(MAKE) e2e-down IDP=keycloak

e2e-up-authentik:
	$(MAKE) e2e-up IDP=authentik

e2e-down-authentik:
	$(MAKE) e2e-down IDP=authentik

e2e-seed-keycloak:
	$(MAKE) e2e-seed IDP=keycloak

e2e-seed-authentik:
	$(MAKE) e2e-seed IDP=authentik

e2e-reset-keycloak:
	$(MAKE) e2e-reset IDP=keycloak

e2e-reset-authentik:
	$(MAKE) e2e-reset IDP=authentik
