FROM golang:1.24 AS builder

WORKDIR /build
COPY . .

RUN CGO_ENABLED=0 go build -o gateway_manager ./cmd/manager
RUN CGO_ENABLED=0 go build -o gateway_proxy ./cmd/proxy

FROM alpine:latest as manager
WORKDIR /
COPY --from=builder /build/gateway_manager /gateway_manager
ENTRYPOINT ["/gateway_manager"]

FROM alpine:latest as proxy
WORKDIR /
COPY --from=builder /build/gateway_proxy /gateway_proxy
ENTRYPOINT ["/gateway_proxy"]