.PHONY: test build tidy install-plugin

test:
	go test ./...

build:
	mkdir -p bin
	go build -o bin/usagebar ./cmd/usagebar
	chmod +x bin/*.sh

tidy:
	go mod tidy

# Link this tree into Herdr as the usagebar plugin (after make build).
install-plugin: build
	herdr plugin link .
