.PHONY: all build test clean proto run docker-build docker-run benchmark

BINARY_NAME=vectorhub
DOCKER_IMAGE=vectorhub:latest
PROTO_PATH=api/proto
GO_FILES=$(shell find . -name '*.go' -not -path "./vendor/*")

all: proto build

build:
	go build -o bin/$(BINARY_NAME) cmd/vectorhub/main.go

proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_PATH)/*.proto

test:
	go test -v -race -coverprofile=coverage.out ./...

test-unit:
	go test -v -race ./internal/...

test-integration:
	go test -v -race ./test/integration/...

test-benchmark:
	go test -bench=. -benchmem ./test/benchmark/...

coverage:
	go tool cover -html=coverage.out

lint:
	golangci-lint run

fmt:
	go fmt ./...
	gofumpt -w $(GO_FILES)

clean:
	rm -rf bin/ coverage.out

docker-build:
	docker build -t $(DOCKER_IMAGE) .

docker-run:
	docker-compose up -d

docker-stop:
	docker-compose down

benchmark:
	go test -bench=. -benchmem -cpuprofile=cpu.prof -memprofile=mem.prof ./test/benchmark/

install-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install mvdan.cc/gofumpt@latest

run:
	go run cmd/vectorhub/main.go -config configs/config.yaml

dev:
	air -c .air.toml