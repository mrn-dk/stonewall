.PHONY: build test vet clean dev

BIN := bin/stonewall

build:
	go build -o $(BIN) ./cmd/stonewall

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf bin .stonewall-data

dev: build
	STONEWALL_RUNTIME=mock STONEWALL_HTTP_ADDR=:8080 ./$(BIN) serve
