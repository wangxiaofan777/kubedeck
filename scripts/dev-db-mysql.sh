#!/usr/bin/env bash
# 本地 MySQL 8 开发/测试库（Docker）。
# 启动一个仅绑定 127.0.0.1 的容器，创建 kubedeck 与 kubedeck_test 两个库，
# 并把连接串写入 .testenv/mysql.env。幂等：重复执行会复用已有容器与密码。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="$ROOT/.testenv/mysql.env"
CONTAINER="kubedeck-mysql-dev"
IMAGE="${MYSQL_IMAGE:-mysql:8.0}"
PORT="${MYSQL_PORT:-3307}"
DB_USER="kubedeck"
ROOT_PASS="kubedeck_root_pw"
DB_PASS=""

if [ -f "$ENV_FILE" ]; then
  DB_PASS="$(sed -n 's#^KUBEDECK_TEST_MYSQL_URL=mysql://[^:]*:\([^@]*\)@.*#\1#p' "$ENV_FILE" | head -1)"
fi
if [ -z "$DB_PASS" ]; then
  DB_PASS="$(openssl rand -hex 16)"
fi

mkdir -p "$ROOT/.testenv"

if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  docker start "$CONTAINER" >/dev/null
  echo "container $CONTAINER started"
else
  docker run -d --name "$CONTAINER" \
    -e "MYSQL_ROOT_PASSWORD=$ROOT_PASS" \
    -e "MYSQL_DATABASE=kubedeck_test" \
    -e "MYSQL_USER=$DB_USER" \
    -e "MYSQL_PASSWORD=$DB_PASS" \
    -p "127.0.0.1:$PORT:3306" \
    "$IMAGE" \
    --character-set-server=utf8mb4 \
    --collation-server=utf8mb4_0900_ai_ci \
    --log-bin-trust-function-creators=1 >/dev/null
  echo "container $CONTAINER created"
fi

echo -n "waiting for mysql"
for _ in $(seq 1 60); do
  if docker exec "$CONTAINER" mysqladmin ping -h 127.0.0.1 -uroot -p"$ROOT_PASS" >/dev/null 2>&1; then
    echo " ready"
    break
  fi
  echo -n "."
  sleep 2
done

docker exec "$CONTAINER" mysql -uroot -p"$ROOT_PASS" -e "
  SET GLOBAL log_bin_trust_function_creators = 1;
  CREATE DATABASE IF NOT EXISTS kubedeck;
  GRANT ALL PRIVILEGES ON kubedeck.* TO '$DB_USER'@'%';
  FLUSH PRIVILEGES;" >/dev/null

umask 077
cat > "$ENV_FILE" <<EOF
KUBEDECK_DATABASE_URL=mysql://$DB_USER:$DB_PASS@127.0.0.1:$PORT/kubedeck
KUBEDECK_TEST_MYSQL_URL=mysql://$DB_USER:$DB_PASS@127.0.0.1:$PORT/kubedeck_test
EOF
chmod 600 "$ENV_FILE"
echo "wrote $ENV_FILE (gitignored)"