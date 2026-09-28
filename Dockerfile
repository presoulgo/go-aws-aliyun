FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /go-aws-aliyun ./cmd/server

FROM alpine:3.22
RUN addgroup -S app && adduser -S app -G app && mkdir /data && chown app:app /data
USER app
WORKDIR /app
COPY --from=builder /go-aws-aliyun ./go-aws-aliyun
ENV OPS_ADDR=:8080 OPS_DB_PATH=/data/ops.db
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["/app/go-aws-aliyun"]
