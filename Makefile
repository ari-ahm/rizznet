# Variables
BINARY_NAME=rizznet
MAIN_PACKAGE=./cmd/rizznet

# Phony targets ensure that make doesn't confuse these commands with file names
.PHONY: all build clean run deps fmt vet help

# Default target
all: build

# Build the binary
build: deps
	@echo "Building $(BINARY_NAME)..."
	CGO_ENABLED=0 go build -o $(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Build complete."

# Run the binary (prints help by default)
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME)

# Clean up binaries and local database
clean:
	@echo "Cleaning up..."
	go clean
	rm -f $(BINARY_NAME)
	rm -f rizznet.db

# Manage dependencies
deps:
	@echo "Tidying and downloading dependencies..."
	go mod tidy
	go mod download

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Static analysis
vet:
	@echo "Vetting code..."
	go vet ./...

update-geoip:
	@echo "Updating GeoIP databases..."
	wget -N https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-ASN.mmdb
	wget -N https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-Country.mmdb

prepare-release: update-geoip
	@echo "Preparing release files..."
	cp config.yaml.example config.yaml

# Help command to list available targets
help:
	@echo "Makefile for $(BINARY_NAME)"
	@echo "Usage:"
	@echo "  make build						- Compile the binary"
	@echo "  make run							- Compile and run the binary"
	@echo "  make clean						- Remove binary and rizznet.db"
	@echo "  make deps						- Update go.mod and download dependencies"
	@echo "  make fmt							- Format Go source files"
	@echo "  make vet							- Run static analysis"
	@echo "  make update-geoip		- Update GeoIP Databases"
	@echo "  make prepare-release	- Prepare for release"
