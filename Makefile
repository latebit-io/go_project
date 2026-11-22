# Shortcuts for those who have make. The gate itself is tools/gate; fmt and
# build are plain tool calls.
.DEFAULT_GOAL := check

.PHONY: check setup init vuln fmt build

check:
	go run ./tools/gate

setup:
	go run ./tools/gate setup

# make init MODULE=github.com/acme/shop
init:
	go run ./tools/gate init "$(MODULE)"

vuln:
	go run ./tools/gate vuln

fmt:
	golangci-lint fmt

build:
	go build -o bin/ ./cmd/...
