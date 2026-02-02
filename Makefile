BINARY := mailcloak
BIN_DIR := bin

.PHONY: build run test tidy clean install

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(BINARY) ./cmd/$(BINARY)

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

tidy:
	go mod tidy

clean:
	rm -f $(BIN_DIR)/$(BINARY)

install: build
	sudo install -m 0755 $(BIN_DIR)/$(BINARY) /usr/local/sbin/$(BINARY)
	sudo install -m 0755 mailcloakctl /usr/local/sbin/mailcloakctl