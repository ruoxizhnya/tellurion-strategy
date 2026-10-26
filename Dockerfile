# syntax=docker/dockerfile:1
# tellurion-strategy · 策略资产服务（ADR-036 G0）
FROM golang:1.25-alpine AS builder

WORKDIR /src
RUN apk add --no-cache git

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o /out/strategy-service ./cmd/assetservice

FROM alpine:3.19

WORKDIR /app
RUN apk add --no-cache ca-certificates netcat-openbsd tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    rm -rf /var/cache/apk/*

COPY --from=builder /out/strategy-service /app/strategy-service

# 非 root 运行（USER nobody）。两处可写目录：
#   - /data/artifacts：compose 挂命名卷；镜像内先建好并 chown，空卷首次挂载会继承该属主。
#   - XDG_CACHE_HOME：不传 -cache-dir 时 defaultCacheDir() 退到这里（/tmp 人人可写），
#     避免 nobody 的 HOME 为 / 时去 mkdir /.cache 失败。
RUN mkdir -p /data/artifacts && chown 65534:65534 /data/artifacts
ENV XDG_CACHE_HOME=/tmp/.cache

EXPOSE 8090
# 本仓今天没有公开 /healthz；G0 用 TCP 探针，公开健康端点归 G1。
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD nc -z 127.0.0.1 8090 || exit 1

USER nobody
ENTRYPOINT ["/app/strategy-service"]