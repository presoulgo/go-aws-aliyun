BINARY  := bin/go-aws-aliyun
VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)
# nomsgpack：去掉 gin 用不到的 msgpack 编解码，二进制小约 6MB
# timetzdata：内置时区数据，在没有 tzdata 的精简系统里也能按 TZ 显示时间
TAGS    := nomsgpack,timetzdata
GO      ?= go

.PHONY: all web build build-go run demo dev test lint docker clean

all: build

## web: 构建前端（首次会安装依赖），产物写入 web/dist 供 go:embed 打包
web:
	@[ -d web/node_modules ] || (cd web && npm ci)
	cd web && npm run build

## build: 构建前端并编译成单个二进制 bin/go-aws-aliyun
build: web build-go

## build-go: 只编译 Go（前端已构建过时使用）
build-go:
	CGO_ENABLED=0 $(GO) build -trimpath -tags $(TAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/server

## run: 编译并启动（读取 configs/config.yaml，不存在时使用默认配置）
run: build
	./$(BINARY)

## demo: 编译并以演示模式启动，不需要云账号
demo: build
	OPS_DEMO=true ./$(BINARY)

## dev: 开发模式：后端以演示模式运行在 :8080，前端 Vite 开发服务器运行在 :5173 并代理 /api
dev:
	@[ -d web/node_modules ] || (cd web && npm ci)
	@trap 'kill 0' EXIT INT TERM; \
		OPS_DEMO=true OPS_ADMIN_PASSWORD=$${OPS_ADMIN_PASSWORD:-Admin12345} $(GO) run -tags $(TAGS) ./cmd/server & \
		cd web && npm run dev

## test: 运行后端测试
test:
	$(GO) test ./...

## lint: 格式、静态检查和前端类型检查
lint:
	@test -z "$$(gofmt -l ./cmd ./internal ./web)" || (gofmt -l ./cmd ./internal ./web; echo "请先运行 gofmt -w"; exit 1)
	$(GO) vet ./...
	@[ -d web/node_modules ] || (cd web && npm ci)
	cd web && npm run typecheck

## docker: 构建镜像 yunshu:$(VERSION)
docker:
	docker build --build-arg VERSION=$(VERSION) -t yunshu:$(VERSION) .

## clean: 删除编译产物（保留 web/dist/.gitkeep）
clean:
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +

help:
	@grep -E '^## ' Makefile | sed 's/^## //'
