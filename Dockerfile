FROM golang:1.24 AS builder

WORKDIR /build
COPY . .

RUN CGO_ENABLED=0 go build -o gateway_manager ./cmd/manager

FROM alpine:latest
WORKDIR /
COPY --from=builder /build/gateway_manager /gateway_manager
ENTRYPOINT ["/gateway_manager"]