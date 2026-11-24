GO ?= go

.PHONY: build run test check
build:
	$(GO) build -trimpath -o bin/todayidid ./cmd/todayidid

run:
	$(GO) run ./cmd/todayidid

test:
	$(GO) test -race ./...

check:
	$(GO) vet ./...
	$(GO) test -race ./...
