# syntax=docker/dockerfile:1

# ---- 前端 ----
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- 后端（纯 Go，无需 CGO） ----
FROM golang:1.26-alpine AS build
ARG VERSION=0.1.0
WORKDIR /src
ENV GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/
COPY --from=web /src/web/dist web/dist
# timetzdata 把时区数据编进二进制，运行镜像不必再安装 tzdata
RUN CGO_ENABLED=0 go build -trimpath -tags "nomsgpack timetzdata" -ldflags "-s -w -X main.version=${VERSION}" -o /out/go-aws-aliyun ./cmd/server

# ---- 运行 ----
# alpine 基础镜像自带 CA 根证书（ca-certificates-bundle），无需联网安装软件包
FROM alpine:3.22
RUN addgroup -S yunshu && adduser -S -G yunshu -h /app yunshu \
    && mkdir -p /app/data /app/configs && chown -R yunshu:yunshu /app
WORKDIR /app
COPY --from=build /out/go-aws-aliyun /app/go-aws-aliyun
COPY configs/config.example.yaml /app/configs/config.example.yaml
ENV TZ=Asia/Shanghai \
    OPS_ADDR=:8080 \
    OPS_DATA_DIR=/app/data
USER yunshu
VOLUME ["/app/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
    CMD wget -qO- http://127.0.0.1:8080/api/v1/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/go-aws-aliyun"]
