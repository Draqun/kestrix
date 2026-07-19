.PHONY: build test clean validate

BINARY := bin/metaci
GO     := go

build:
	$(GO) build -o $(BINARY) ./cmd/metaci

test:
	$(GO) test ./... -v

clean:
	rm -rf bin/

validate:
	$(BINARY) validate examples/pipeline.tf

resolve:
	$(BINARY) resolve $(ARGS) examples/pipeline.tf

.DEFAULT_GOAL := build
