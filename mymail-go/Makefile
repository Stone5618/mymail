# MyMail Go 后端 Makefile
# 用法：make help 查看所有命令

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GOFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: all build test lint run dev docker help clean tidy

all: lint test build

# ===== 构建 =====
build:
	CGO_ENABLED=0 go build $(GOFLAGS) -o dist/mymail ./cmd/mymail
	@echo "已构建 dist/mymail (版本: $(VERSION))"

build-all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o dist/mymail-linux-amd64 ./cmd/mymail
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -o dist/mymail-linux-arm64 ./cmd/mymail
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -o dist/mymail-darwin-amd64 ./cmd/mymail
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -o dist/mymail-windows-amd64.exe ./cmd/mymail
	@echo "已构建全部平台二进制"

# ===== 依赖管理 =====
tidy:
	go mod tidy

# ===== 测试 =====
test:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -func=coverage.out | grep total

test-short:
	go test -short -race ./...

test-integration:
	go test -tags=integration ./tests/integration/...

test-e2e:
	go test -tags=e2e ./tests/e2e/...

bench:
	go test -bench=. -benchmem ./...

# ===== 代码质量 =====
lint:
	golangci-lint run --timeout=5m

lint-fix:
	golangci-lint run --fix --timeout=5m

security:
	govulncheck ./...
	gosec -severity medium -confidence medium ./...

# ===== 运行 =====
dev:
	go run ./cmd/mymail

run:
	./dist/mymail

# ===== Docker =====
# 构建上下文为项目根目录（..），以访问 mymail-go/ 和 mymail-vue/
docker-build:
	docker build -t mymail:$(VERSION) -f deployments/docker/Dockerfile ..

docker-up:
	docker compose -f deployments/docker/docker-compose.yml up -d

docker-down:
	docker compose -f deployments/docker/docker-compose.yml down

docker-logs:
	docker compose -f deployments/docker/docker-compose.yml logs -f

# ===== 数据库 =====
migrate:
	go run ./cmd/mymail -migrate

# ===== 清理 =====
clean:
	rm -rf dist/ coverage.out

# ===== 帮助 =====
help:
	@echo "MyMail Go 后端构建命令"
	@echo ""
	@echo "构建:"
	@echo "  build          构建二进制到 dist/mymail"
	@echo "  build-all      构建全平台二进制"
	@echo "  tidy           整理依赖 (go mod tidy)"
	@echo ""
	@echo "测试:"
	@echo "  test           运行单元测试（含覆盖率）"
	@echo "  test-short     运行短测试"
	@echo "  test-integration 运行集成测试"
	@echo "  test-e2e       运行端到端测试"
	@echo "  bench          运行性能基准测试"
	@echo ""
	@echo "代码质量:"
	@echo "  lint           运行 golangci-lint"
	@echo "  lint-fix       自动修复 lint 问题"
	@echo "  security       运行安全扫描 (govulncheck + gosec)"
	@echo ""
	@echo "运行:"
	@echo "  dev            开发模式运行 (go run)"
	@echo "  run            运行已构建的二进制"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build   构建 Docker 镜像"
	@echo "  docker-up      启动 Docker Compose"
	@echo "  docker-down    停止 Docker Compose"
	@echo "  docker-logs    查看 Docker 日志"
	@echo ""
	@echo "其他:"
	@echo "  clean          清理构建产物"
	@echo "  help           显示此帮助"
