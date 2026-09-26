VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS  = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build test lint rules dashboards metrics-doc smoke

build:
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o bin/tvheadend-exporter ./cmd/tvheadend-exporter

test:
	go test ./... -race -count=1

lint:
	go vet ./...
	golangci-lint run ./...

rules:
	promtool check rules deploy/prometheus/rules/*.yml
	cd deploy/prometheus && promtool test rules rules_test.yml

dashboards:
	python3 hack/gen_dashboards.py

metrics-doc: build
	TVH_URL=http://localhost TVH_USERNAME=x TVH_PASSWORD=x TVH_GEOIP_DB=internal/testdata/GeoIP2-City-Test.mmdb ./bin/tvheadend-exporter -dump-metrics > docs/METRICS.md

smoke: build
	./bin/tvheadend-exporter -listen 127.0.0.1:9429 & pid=$$!; sleep 15; \
	curl -sf http://127.0.0.1:9429/metrics | grep -E '^tvheadend_up 1' ; rc=$$?; kill $$pid; exit $$rc
