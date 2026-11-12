GO ?= go
NPM ?= npm
BIN := bin/kubedeck
WEB := web

.PHONY: build web server dev test test-db db lint clean

build: web server

# 本地开发库：创建角色与 kubedeck/kubedeck_test 库，连接串写入 .testenv/db.env
db:
	bash scripts/dev-db.sh

# 前端构建产物供 go:embed 使用，必须先于 server 执行。
web:
	cd $(WEB) && $(NPM) ci --no-audit --no-fund && $(NPM) run build

server:
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/server

dev:
	$(GO) run ./cmd/server

test:
	$(GO) test ./...

# 数据层集成测试：连真实 PostgreSQL，每个用例重置 public schema
test-db:
	set -a; . ./.testenv/db.env; set +a; $(GO) test ./internal/store/ -v

lint:
	$(GO) vet ./...

clean:
	rm -rf $(BIN) $(WEB)/dist/assets $(WEB)/dist/index.html $(WEB)/dist/vite.svg