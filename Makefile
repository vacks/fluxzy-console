SHELL := /bin/sh
GO_IMAGE ?= golang:1.27-alpine
NODE_IMAGE ?= node:22-alpine
GO_CACHE_VOLUME ?= fluxzy-console-go-cache
include fork/fluxzy/upstream.env
FLUXZY_CLI_RELEASE ?= $(FLUXZY_VERSION)
CONSOLE_VERSION := $(strip $(shell cat VERSION))
HOST_UID := $(shell id -u)
HOST_GID := $(shell id -g)
GO_DOCKER = docker run --rm -v "$(CURDIR)":/src -v "$(GO_CACHE_VOLUME)":/go/pkg/mod -w /src $(GO_IMAGE)

COMPOSE = docker compose -f docker-compose.yml
DEV_COMPOSE = docker compose -p fluxzy-console-dev -f docker-compose.yml -f docker-compose.dev.yml

.PHONY: help web typecheck web-dist-placeholder test vet fmt build docker-build up up-build down logs dev-build dev-up dev-down dev-logs dev-reset clean

help:
	@echo "web           build the Vue frontend in a Node container"
	@echo "typecheck     run vue-tsc"
	@echo "test          run Go tests in a Go container"
	@echo "vet           run go vet and gofmt check in a Go container"
	@echo "fmt           gofmt the Go sources"
	@echo "build         build the console binary (current arch) into bin/console"
	@echo "docker-build  build the linux/amd64 production image from source"
	@echo "up            pull and start the released production image"
	@echo "up-build      build the production image from source and start it"
	@echo "down          docker compose down"
	@echo "logs          docker compose logs -f"
	@echo "dev-up        build and start the arm64 dev stack"
	@echo "dev-down      stop the dev stack without deleting volumes"
	@echo "dev-reset     remove only this project's dev volumes"
	@echo "dev-logs      follow dev stack logs"
	@echo "fork-build-arm64  build the patched fluxzy CLI for dev arm64"
	@echo "fork-build-amd64  build the patched fluxzy CLI for production musl amd64"
	@echo "fork-check    validate fork patches against an upstream release"

web:
	docker run --rm -u "$(HOST_UID):$(HOST_GID)" -e HOME=/tmp -e npm_config_cache=/tmp/npm-cache \
		-v "$(CURDIR)/web":/app -w /app $(NODE_IMAGE) \
		sh -c 'npm ci --no-audit --no-fund && npm run build'

typecheck:
	docker run --rm -u "$(HOST_UID):$(HOST_GID)" -e HOME=/tmp -e npm_config_cache=/tmp/npm-cache \
		-v "$(CURDIR)/web":/app -w /app $(NODE_IMAGE) \
		sh -c 'npm ci --no-audit --no-fund && npm run typecheck'

web-dist-placeholder:
	mkdir -p web/dist
	test -f web/dist/index.html || printf '<!doctype html><title>fluxzy-console</title>' > web/dist/index.html

test: web-dist-placeholder
	$(GO_DOCKER) sh -c 'go test ./...'

vet:
	$(GO_DOCKER) sh -c 'go vet ./... && test -z "$$(gofmt -l $$(find . -name "*.go" -not -path "./web/node_modules/*" -not -path "./bin/*"))"'

fmt:
	$(GO_DOCKER) sh -c 'gofmt -w $$(find . -name "*.go" -not -path "./web/node_modules/*" -not -path "./bin/*")'

build: web
	$(GO_DOCKER) sh -c 'CGO_ENABLED=0 go build -trimpath -o bin/console ./cmd/console'

docker-build:
	docker buildx build --platform linux/amd64 --load \
		-f deploy/Dockerfile --target prod-amd64 \
		--build-arg CONSOLE_VERSION=$(CONSOLE_VERSION) \
		--build-arg FLUXZY_CLI_RELEASE=$(FLUXZY_CLI_RELEASE) \
		--build-arg HTTP_PROXY="$(HTTP_PROXY)" \
		--build-arg HTTPS_PROXY="$(HTTPS_PROXY)" \
		--build-arg NO_PROXY="$(NO_PROXY)" \
		-t ghcr.io/vacks/fluxzy-console:$(CONSOLE_VERSION) \
		-t fluxzy-console:$(CONSOLE_VERSION) .

up:
	$(COMPOSE) pull
	$(COMPOSE) up -d

up-build: docker-build
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

dev-build: $(FORK_ARM64_STAMP)
	$(DEV_COMPOSE) build

dev-up: $(FORK_ARM64_STAMP)
	$(DEV_COMPOSE) up -d --build

dev-down:
	$(DEV_COMPOSE) down

dev-logs:
	$(DEV_COMPOSE) logs -f

# Remove only volumes created by this project. Never use docker volume prune.
dev-reset:
	$(DEV_COMPOSE) down
	docker volume rm -f \
		fluxzy-console-dev_fluxzy-data \
		fluxzy-console-dev_console-data

FORK_ARM64_STAMP := fork/fluxzy/out/linux-arm64/fluxzy
FORK_AMD64_STAMP := fork/fluxzy/out/linux-amd64/fluxzy
FORK_INPUTS := $(wildcard fork/fluxzy/patches/*.patch) fork/fluxzy/build.sh

$(FORK_ARM64_STAMP): $(FORK_INPUTS)
	./fork/fluxzy/build.sh arm64

$(FORK_AMD64_STAMP): $(FORK_INPUTS)
	./fork/fluxzy/build.sh amd64

fork-build-arm64: $(FORK_ARM64_STAMP)
fork-build-amd64: $(FORK_AMD64_STAMP)

fork-check:
	./fork/fluxzy/check-upstream.sh $(FLUXZY_CLI_RELEASE)

dev-up: $(FORK_ARM64_STAMP)
docker-build: $(FORK_AMD64_STAMP)
dev-build: $(FORK_ARM64_STAMP)

clean:
	rm -rf bin web/dist
