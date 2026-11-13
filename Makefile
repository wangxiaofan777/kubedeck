SHELL := /bin/bash

# WSL 下 go 可能不在 PATH；找不到时回退到标准安装位置。
GO ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)
GO_DIR := $(dir $(GO))
export PATH := $(GO_DIR):$(PATH)
NPM ?= npm
BIN := bin/kubedeck
WEB := web
DIST := internal/webui/dist
KEEP := $(DIST)/.gitkeep
STATICCHECK ?= staticcheck
# 显式包范围：避免扫描 web/node_modules（npm 依赖里可能带 Go 文件）。
PKGS := ./cmd/... ./internal/...
COMPOSE := docker compose -f deploy/compose/docker-compose.yml

.PHONY: all build web server dev fmt lint test test-db db web-check check web-dev clean test-rules compose-up compose-down compose-config

# 本地默认：先检查再构建。CI 使用各自独立的目标。
all: check build

build: web server

# 前端构建。构建后补回占位文件，保证 go:embed 始终有匹配。
web:
	cd $(WEB) && $(NPM) ci --no-audit --no-fund
	cd $(WEB) && $(NPM) run build
	@mkdir -p $(DIST) && touch $(KEEP)

# 前端产物占位：允许在未构建前端时编译后端（go:embed all:dist 需要目录非空）。
$(KEEP):
	@mkdir -p $(DIST) && touch $(KEEP)

server: $(KEEP)
	@mkdir -p bin
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/server

# 只启动后端；需要前端最新产物时先执行 make web。
dev: $(KEEP)
	$(GO) run ./cmd/server

web-dev: $(KEEP)
	cd $(WEB) && $(NPM) run dev

fmt:
	$(GO) fmt $(PKGS)
	cd $(WEB) && $(NPM) run format

# 后端静态检查：格式、vet、staticcheck。任一项失败即非零退出。
lint: $(KEEP)
	@files="$$(gofmt -l cmd internal)"; \
	if [ -n "$$files" ]; then echo "gofmt required for:"; echo "$$files"; exit 1; fi
	$(GO) vet $(PKGS)
	@command -v $(STATICCHECK) >/dev/null 2>&1 || { \
		echo "staticcheck not found. Install: go install honnef.co/go/tools/cmd/staticcheck@2026.2.2"; exit 1; }
	$(STATICCHECK) $(PKGS)

test: $(KEEP)
	$(GO) test $(PKGS)

test-db: $(KEEP)
	set -a; . ./.testenv/db.env; set +a; $(GO) test ./internal/store/ -v

# PrometheusRule 语法与告警触发单元测试（需要 python3 + PyYAML + promtool）。
test-rules:
	bash scripts/check-prometheus-rules.sh

# Docker Compose 单机部署（需 deploy/compose/.env）。
compose-config:
	$(COMPOSE) config

compose-up:
	$(COMPOSE) up -d --build

compose-down:
	$(COMPOSE) down

db:
	bash scripts/dev-db.sh

web-check:
	cd $(WEB) && $(NPM) run lint && $(NPM) run typecheck

# 提交前统一入口：后端 + 前端检查与测试。
check: lint test web-check

clean:
	rm -rf $(BIN) $(DIST)/assets
	rm -f $(DIST)/index.html
	@mkdir -p $(DIST) && touch $(KEEP)