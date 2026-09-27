# Carga las variables de .env (si existe) para make run/dev/migrate.
-include .env
export

.PHONY: run dev migrate-up migrate-down migrate-version test test-integration lint build
run:
	go run ./cmd/server
dev:
	air -c .air.toml
migrate-up:
	go run ./cmd/migrations up
migrate-down:
	go run ./cmd/migrations down
migrate-version:
	go run ./cmd/migrations version
test:
	go test ./...
# Requiere docker compose up (Redis en localhost:6379).
test-integration:
	QATU_TEST_REDIS_ADDR=localhost:6379 go test -count=1 ./...
lint:
	go vet ./...
build:
	go build -o bin/server ./cmd/server
