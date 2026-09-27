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
# Requiere docker compose up. Postgres: crea una base temporal por prueba y la borra al terminar.
test-integration:
	QATU_TEST_REDIS_ADDR=localhost:6379 \n	QATU_TEST_DATABASE_URL=postgres://qatu:qatu@localhost:5433/qatu?sslmode=disable \n	QATU_TEST_MAILPIT=localhost \n	go test -count=1 ./...
lint:
	go vet ./...
build:
	go build -o bin/server ./cmd/server
