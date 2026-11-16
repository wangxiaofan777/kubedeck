#!/usr/bin/env bash
# 离线交付打包：构建镜像、导出为 gzip、生成校验和与部署文件。
#
# 在联网机器执行；目标机只需要 Docker（Compose）或 containerd（Kubernetes）。
# 用法见 docs/offline-deployment.md，或执行 --help。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="$ROOT/deploy/helm/kubedeck"
COMPOSE_DIR="$ROOT/deploy/compose"

VERSION=""
PLATFORM="linux/amd64"
OUT="$ROOT/dist/offline"
INCLUDE_DEPS=1
WITH_MYSQL=0
DO_BUILD=1
REGISTRY=""

POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:17-alpine}"
MYSQL_IMAGE="${MYSQL_IMAGE:-mysql:8.0}"
CADDY_IMAGE="${CADDY_IMAGE:-caddy:2-alpine}"

usage() {
  cat <<'EOF'
用法: scripts/offline-bundle.sh [选项]

  --version VERSION    镜像 tag，默认取 Chart.yaml 的 appVersion
  --platform PLATFORM  目标平台：linux/amd64（默认）或 linux/arm64
  --out DIR            输出目录，默认 dist/offline
  --no-deps            只导出 KubeDeck 镜像（适合 Helm + 外部数据库）
  --with-mysql         依赖镜像中包含 MySQL 8（默认只带 PostgreSQL）
  --no-build           跳过构建，导出本地已有的 kubedeck:VERSION 镜像
  --registry HOST      额外生成推送到内网 registry 的脚本，例如 harbor.example.com/kubedeck
  -h, --help           显示本帮助

环境变量:
  POSTGRES_IMAGE       默认 postgres:17-alpine
  MYSQL_IMAGE          默认 mysql:8.0（仅 --with-mysql 时打包）
  CADDY_IMAGE          默认 caddy:2-alpine
EOF
}

die() {
  echo "错误: $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令: $1"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2:-}"; shift 2 ;;
    --platform) PLATFORM="${2:-}"; shift 2 ;;
    --out) OUT="${2:-}"; shift 2 ;;
    --no-deps) INCLUDE_DEPS=0; shift ;;
    --with-mysql) WITH_MYSQL=1; shift ;;
    --no-build) DO_BUILD=0; shift ;;
    --registry) REGISTRY="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数: $1（--help 查看用法）" ;;
  esac
done

if [[ -z "$VERSION" ]]; then
  VERSION="$(sed -n 's/^appVersion:[[:space:]]*"\?\([^"]*\)"\?.*/\1/p' "$CHART_DIR/Chart.yaml" | head -1)"
  [[ -n "$VERSION" ]] || die "无法从 Chart.yaml 读取 appVersion，请显式传 --version"
fi

case "$PLATFORM" in
  linux/amd64|linux/arm64) ;;
  *) die "暂只支持 linux/amd64 与 linux/arm64，收到: $PLATFORM" ;;
esac
PLATFORM_TAG="$(echo "$PLATFORM" | tr '/' '-')"

APP_IMAGE="kubedeck:$VERSION"
need docker
need gzip
if command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  SHA_CMD=(shasum -a 256)
else
  die "缺少 sha256sum/shasum，无法生成校验和"
fi

mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"

GIT_REV="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
MANIFEST="$OUT/manifest.txt"
SUM_FILE="$OUT/SHA256SUMS"

echo "==> KubeDeck 离线包"
echo "    版本: $VERSION"
echo "    平台: $PLATFORM"
echo "    输出: $OUT"

if [[ "$DO_BUILD" -eq 1 ]]; then
  need docker
  docker buildx version >/dev/null 2>&1 || die "缺少 docker buildx；升级 Docker 或安装 buildx 插件"
  echo "==> 构建镜像 $APP_IMAGE（$PLATFORM）"
  docker buildx build --platform "$PLATFORM" --provenance=false --sbom=false \
    --tag "$APP_IMAGE" --load "$ROOT"
else
  docker image inspect "$APP_IMAGE" >/dev/null 2>&1 \
    || die "本地不存在 $APP_IMAGE；先构建或去掉 --no-build"
fi

IMAGES=("$APP_IMAGE")
if [[ "$INCLUDE_DEPS" -eq 1 ]]; then
  IMAGES+=("$POSTGRES_IMAGE" "$CADDY_IMAGE")
  if [[ "$WITH_MYSQL" -eq 1 ]]; then
    IMAGES+=("$MYSQL_IMAGE")
  fi
  for img in "${IMAGES[@]:1}"; do
    if ! docker image inspect "$img" >/dev/null 2>&1; then
      echo "==> 拉取 $img"
      docker pull --platform "$PLATFORM" "$img"
    fi
  done
fi

save_image() {
  local ref="$1" file="$2" tmp
  tmp="$file.tmp"
  echo "==> 导出 $ref -> $(basename "$file")"
  # 先落盘再压缩，避免 Windows/Git Bash 下二进制管道被终端转换。
  docker save --output "$tmp" "$ref"
  gzip -9 -n -c "$tmp" > "$file"
  rm -f "$tmp"
}

: > "$SUM_FILE"
: > "$MANIFEST"

for img in "${IMAGES[@]}"; do
  safe_name="$(echo "$img" | tr '/:@' '---')"
  file="$OUT/${safe_name}-${PLATFORM_TAG}.tar.gz"
  save_image "$img" "$file"
  (cd "$OUT" && "${SHA_CMD[@]}" "$(basename "$file")" >> "$SUM_FILE")
done

echo "==> 复制部署文件"
cp "$COMPOSE_DIR/docker-compose.yml" "$OUT/"
cp "$COMPOSE_DIR/docker-compose.mysql.yml" "$OUT/"
cp "$COMPOSE_DIR/docker-compose.offline.yml" "$OUT/"
cp "$COMPOSE_DIR/Caddyfile.offline" "$OUT/"
cp "$COMPOSE_DIR/.env.example" "$OUT/env.example"
cp "$COMPOSE_DIR/.env.mysql.example" "$OUT/env.mysql.example"
cp "$ROOT/docs/offline-deployment.md" "$OUT/"
tar -czf "$OUT/kubedeck-chart-$VERSION.tar.gz" -C "$(dirname "$CHART_DIR")" "$(basename "$CHART_DIR")"
cp "$ROOT/deploy/helm/kubedeck/values.yaml" "$OUT/helm-values.yaml"
(cd "$OUT" && "${SHA_CMD[@]}" "kubedeck-chart-$VERSION.tar.gz" >> "$SUM_FILE")

cat > "$OUT/load-images.sh" <<'LOAD'
#!/usr/bin/env bash
# 在目标机执行：校验并导入本目录下的所有镜像。
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c SHA256SUMS
else
  shasum -a 256 -c SHA256SUMS
fi

while IFS= read -r f; do
  case "$f" in
    kubedeck-chart-*) continue ;;
    *.tar.gz) ;;
    *) continue ;;
  esac
  echo "==> docker load -i $f"
  docker load -i "$f"
done < <(awk '{print $2}' SHA256SUMS | sed 's/^\*//')
LOAD
chmod +x "$OUT/load-images.sh"

if [[ -n "$REGISTRY" ]]; then
  REGISTRY="${REGISTRY%/}"
  cat > "$OUT/push-images.sh" <<PUSH
#!/usr/bin/env bash
# 在能访问内网 registry 的机器执行：重新打 tag 并推送。
set -euo pipefail
REGISTRY="$REGISTRY"
VERSION="$VERSION"

docker tag "kubedeck:\$VERSION" "\$REGISTRY/kubedeck:\$VERSION"
docker push "\$REGISTRY/kubedeck:\$VERSION"
PUSH
  if [[ "$INCLUDE_DEPS" -eq 1 ]]; then
    cat >> "$OUT/push-images.sh" <<PUSH
for img in "$POSTGRES_IMAGE" "$CADDY_IMAGE"; do
  name="\${img%%:*}"
  tag="\${img##*:}"
  docker tag "\$img" "\$REGISTRY/\$name:\$tag"
  docker push "\$REGISTRY/\$name:\$tag"
done
PUSH
    if [[ "$WITH_MYSQL" -eq 1 ]]; then
      cat >> "$OUT/push-images.sh" <<PUSH
img="$MYSQL_IMAGE"
name="\${img%%:*}"
tag="\${img##*:}"
docker tag "\$img" "\$REGISTRY/\$name:\$tag"
docker push "\$REGISTRY/\$name:\$tag"
PUSH
    fi
  fi
  chmod +x "$OUT/push-images.sh"
fi

{
  echo "KubeDeck offline bundle"
  echo "version:      $VERSION"
  echo "platform:     $PLATFORM"
  echo "built_at:     $BUILD_TIME"
  echo "git_revision: $GIT_REV"
  echo "images:"
  for img in "${IMAGES[@]}"; do
    inspect="$(docker image inspect "$img" --format '{{.Id}} size={{.Size}}' 2>/dev/null || echo 'not found')"
    echo "  - $img ($inspect)"
  done
  echo "files:"
  while IFS= read -r f; do
    echo "  - $(basename "$f") ($(wc -c < "$f") bytes)"
  done < <(find "$OUT" -maxdepth 1 -type f ! -name manifest.txt ! -name SHA256SUMS | sort)
  echo "sha256:"
  sed 's/^/  /' "$SUM_FILE"
} > "$MANIFEST"

echo
echo "==> 完成。包内文件："
(cd "$OUT" && ls -lh | sed 's/^/    /')
echo
echo "传输到目标机后执行："
echo "    cd <目标目录> && ./load-images.sh"
echo
echo "总传输体积（gzip）："
du -ch "$OUT"/*.tar.gz 2>/dev/null | tail -1 | sed 's/^/    /'
echo
echo "部署步骤见 $OUT/offline-deployment.md"