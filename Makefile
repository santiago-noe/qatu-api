# Carga las variables de .env (si existe) para make run/dev/migrate.
-include .env
export

.PHONY: run dev migrate-up migrate-down test lint build
run:
	go run ./cmd/server
dev:
	air -c .air.toml
migrate-up:
	go run ./cmd/migrations up
migrate-down:
	go run ./cmd/migrations down
test:
	go test ./...
lint:
	go vet ./...
build:
	go build -o bin/server ./cmd/server
