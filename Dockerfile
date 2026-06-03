FROM golang:1.25 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/crypter-api ./cmd/api

FROM gcr.io/distroless/base-debian12

WORKDIR /app
COPY --from=builder /bin/crypter-api /app/crypter-api
COPY --from=builder /app/migrations /app/migrations
COPY --from=builder /app/web /app/web

EXPOSE 8080
ENTRYPOINT ["/app/crypter-api"]
