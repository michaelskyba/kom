BINARY_NAME=shell-exec
BUILD_DIR=bin

.PHONY: build test install clean

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

test:
	go test ./...

install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin/"
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/

clean:
	@rm -rf $(BUILD_DIR)