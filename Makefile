.PHONY: build build-dashboard test vet clean dev

BIN := bin/stonewall

# build-dashboard compiles the SPA into dashboard/build. The default `build`
# target runs it first so the single binary embeds the dashboard.
build-dashboard:
	cd dashboard && npm install --no-audit --no-fund && npm run build

build: build-dashboard
	go build -tags dashboard -o $(BIN) ./cmd/stonewall

# test runs without the dashboard tag (the SPA is not built in CI/quick-test),
# so the embedded-dashboard package compiles as an empty stub.
test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf bin .stonewall-data dashboard/build

dev: build
	STONEWALL_RUNTIME=mock STONEWALL_HTTP_ADDR=:8080 ./$(BIN) serve
