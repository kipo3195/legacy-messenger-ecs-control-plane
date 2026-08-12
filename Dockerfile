FROM golang:1.26.4 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o control-plane ./cmd/server/main.go


FROM debian:bookworm-slim

WORKDIR /app

COPY --from=builder /app/control-plane ./control-plane
COPY --from=builder /app/configs ./configs

EXPOSE 8080

ENTRYPOINT ["./control-plane"]