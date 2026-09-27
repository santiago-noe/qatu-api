FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -o /out/migrations ./cmd/migrations \
 && CGO_ENABLED=0 go build -o /out/admin ./cmd/admin

FROM alpine:3.22
# Las migraciones van incrustadas en el binario /app/migrations (go:embed).
WORKDIR /app
COPY --from=build /out/ /app/
COPY internal/adapter/outbound/smtp/templates /app/templates
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh
EXPOSE 8080
ENTRYPOINT ["/app/entrypoint.sh"]
