SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO ?= go
TFPLUGINDOCS := $(GO) run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0
GOLANGCI := $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0

.PHONY: build test testacc docs docs-check lint fmt gen check clean

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/terraform-provider-harbor-pilot .

test:
	$(GO) test -count=1 ./...

# Acceptance tests build harbor-pilot from ../go-harbor-pilot (HP_APP_DIR) and run it.
testacc:
	TF_ACC=1 $(GO) test -count=1 -timeout 30m ./...

# Regenerate the spec schemas from the vendored OpenAPI document.
gen:
	$(GO) generate ./internal/specschema

docs:
	$(TFPLUGINDOCS) generate --provider-name harborpilot

docs-check: docs
	git diff --exit-code docs/

lint:
	$(GO) vet ./...
	$(GOLANGCI) run ./...
	terraform fmt -check -recursive examples || true

fmt:
	gofmt -w .
	terraform fmt -recursive examples

check: lint test docs-check

clean:
	rm -rf bin dist
