.PHONY: build run test vet tidy lint docker-up docker-down

build:
	go build -o bin/gateway ./cmd/gateway

run:
	go run ./cmd/gateway -config configs/config.yaml

test:
	go test ./... -cover

vet:
	go vet ./...

tidy:
	go mod tidy

lint: vet
	@echo "lint ok (add golangci-lint as needed)"

docker-up:
	docker compose -f deployments/docker-compose.yaml up --build

docker-down:
	docker compose -f deployments/docker-compose.yaml down
