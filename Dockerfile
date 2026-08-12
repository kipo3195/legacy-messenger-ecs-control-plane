FROM golang:1.26.4 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o control-plane ./cmd/server/main.go


FROM debian:bookworm-slim

# AWS HTTPS 인증서 검증에 필요
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/control-plane ./control-plane
COPY --from=builder /app/configs ./configs

EXPOSE 8080

ENTRYPOINT ["./control-plane"]