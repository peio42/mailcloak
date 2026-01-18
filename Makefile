BINARY := kc-policy
BIN_DIR := bin

.PHONY: build run test tidy clean install

build:
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/$(BINARY)

run:
	go run ./cmd/$(BINARY)

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -f $(BIN_DIR)/$(BINARY)

install: build
	sudo install -m 0755 $(BIN_DIR)/$(BINARY) /usr/local/sbin/$(BINARY)
