# ObservaBoard — development tasks.
# Requirements: Go 1.24+, Docker (for test-configs and e2e).

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
LDFLAGS := -s -w -X github.com/root-lr/observaboard/internal/version.Version=$(VERSION)
BIN     := bin/observa

# Images used to validate the configurations (same versions as internal/version).
PROM_IMG  := prom/prometheus:v3.15.1
AM_IMG    := prom/alertmanager:v0.34.1
CADDY_IMG := caddy:2.11
LOKI_IMG  := grafana/loki:3.7.9
ALLOY_IMG := grafana/alloy:v1.21.2

# The generated central directory is private (0700): run the checks as the current user.
AS_ME := --user "$$(id -u):$$(id -g)"

.PHONY: build test vet render test-rules test-configs e2e dist clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/observa

vet:
	go vet ./...

test: vet
	go test ./...

# Generates a reference central and agent in build/ (no Docker).
render: build
	rm -rf build && mkdir -p build
	$(BIN) --dry-run central install --dir build/central --domain example.com --first-agent ci > /dev/null
	$(BIN) --dry-run agent install --dir build/agent --central https://example.com --token ci.secret --host ci > /dev/null
	cp -r examples/django-shop build/shop
	$(BIN) init --dir build/shop --project boutique --env prod --yes > /dev/null
	$(BIN) --dry-run apply build/shop/observa.yaml --agent-dir build/agent > /dev/null

# Alert rule unit tests (local promtool, otherwise via Docker).
test-rules: render
	@if command -v promtool >/dev/null; then promtool test rules tests/rules/observa.test.yml; \
	else docker run --rm $(AS_ME) -v "$(CURDIR)":/w -w /w --entrypoint promtool $(PROM_IMG) test rules tests/rules/observa.test.yml; fi

# Validates each generated configuration with the component's official binary.
test-configs: render
	docker run --rm $(AS_ME) -v "$(CURDIR)/build/central":/c -w /c --entrypoint promtool $(PROM_IMG) check rules prometheus/rules/observa.yml
	docker run --rm $(AS_ME) -v "$(CURDIR)/build/central":/c --entrypoint amtool $(AM_IMG) check-config /c/alertmanager/alertmanager.yml
	docker run --rm -v "$(CURDIR)/build/central/caddy":/etc/caddy $(CADDY_IMG) caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
	docker run --rm -v "$(CURDIR)/build/central/loki":/etc/loki $(LOKI_IMG) -config.file=/etc/loki/loki.yaml -verify-config
	docker run --rm -v "$(CURDIR)/build/agent/config":/a $(ALLOY_IMG) fmt /a/config.alloy > /dev/null
	docker compose -f build/central/docker-compose.yml config -q
	docker compose -f build/agent/docker-compose.yml config -q
	docker compose -f build/shop/compose.yaml -f build/shop/docker-compose.observa.yml config -q
	@echo "All configurations are valid."

# End-to-end test: all-in-one stack + shop + simulated failure (5 to 15 minutes).
e2e: build
	tests/e2e/run.sh $(BIN)

# Release binaries for Linux, macOS and Windows.
dist:
	rm -rf dist && mkdir -p dist
	for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
		os=$${target%/*}; arch=$${target#*/}; ext=""; [ $$os = windows ] && ext=".exe"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/observa-$$os-$$arch$$ext ./cmd/observa; \
	done
	cd dist && sha256sum observa-* > checksums.txt

clean:
	rm -rf bin build dist
