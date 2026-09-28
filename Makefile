.PHONY: build dev test clean

build:
	cd web && npm ci && npm run build && npm run typecheck
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/go-aws-aliyun ./cmd/server

dev:
	cd web && npm run dev

test:
	go test ./...

clean:
	go clean -cache
